package summary

import (
	"context"
	"fmt"
	"log/slog"
	"strings"
	"time"

	"bot-summary-vk/internal/config"
	"bot-summary-vk/internal/llm"
	"bot-summary-vk/internal/storage"
)

type Publisher interface {
	Publish(ctx context.Context, peerID int64, text string) error
	PublishFormatted(ctx context.Context, peerID int64, text string, formatData string) error
	PublishFormattedWithImage(ctx context.Context, peerID int64, text string, formatData string, image []byte) error
}

type ImageGenerator interface {
	GenerateSummaryImage(ctx context.Context, summaryText string) ([]byte, error)
}

type TriggerSource string

const (
	TriggerSourceAuto   TriggerSource = "auto_batch"
	TriggerSourceManual TriggerSource = "manual_command"
)

type RunStatus string

const (
	RunStatusPublished         RunStatus = "published"
	RunStatusLocked            RunStatus = "locked"
	RunStatusAlreadyProcessed  RunStatus = "already_processed"
	RunStatusNotEnoughMessages RunStatus = "not_enough_messages"
	RunStatusNoMessages        RunStatus = "no_messages"
	RunStatusRateLimited       RunStatus = "rate_limited"
)

type RunResult struct {
	Status          RunStatus
	Trigger         TriggerSource
	FirstMessageID  int64
	LastMessageID   int64
	RangeStart      time.Time
	RangeEnd        time.Time
	RawCount        int
	MeaningfulCount int
	RequiredCount   int
	SummaryText     string
}

type Service struct {
	repo          *storage.Repository
	llmClient     llm.Client
	publisher     Publisher
	prepareConfig PrepareConfig
	promptBuilder PromptBuilder
	logger        *slog.Logger
	imageGen      ImageGenerator
	batchSize     int
	fetchLimit    int
	maxOutput     int
}

func NewService(repo *storage.Repository, llmClient llm.Client, publisher Publisher, cfg config.Config, logger *slog.Logger, imageGen ImageGenerator) *Service {
	fetchLimit := cfg.Summary.BatchSize * 3
	if fetchLimit < 500 {
		fetchLimit = 500
	}

	return &Service{
		repo:      repo,
		llmClient: llmClient,
		publisher: publisher,
		prepareConfig: PrepareConfig{
			MinMessageLength:   cfg.Summary.MinMessageLength,
			MaxContextChars:    cfg.Summary.MaxContextChars,
			MaxContextMessages: cfg.Summary.MaxContextMessages,
		},
		promptBuilder: NewPromptBuilder(cfg.LLM.PromptMaxChars),
		logger:        logger,
		imageGen:      imageGen,
		batchSize:     cfg.Summary.BatchSize,
		fetchLimit:    fetchLimit,
		maxOutput:     cfg.LLM.MaxOutputTokens,
	}
}

func (s *Service) ExecuteAuto(ctx context.Context, chatID, peerID int64) (RunResult, error) {
	return s.executeNext(ctx, chatID, peerID, TriggerSourceAuto)
}

func (s *Service) ExecuteManual(ctx context.Context, chatID, peerID int64) (RunResult, error) {
	return s.executeNext(ctx, chatID, peerID, TriggerSourceManual)
}

func (s *Service) executeNext(ctx context.Context, chatID, peerID int64, trigger TriggerSource) (RunResult, error) {
	state, err := s.repo.GetSummaryChatState(ctx, chatID, peerID, s.batchSize)
	if err != nil {
		return RunResult{}, fmt.Errorf("load summary chat state: %w", err)
	}

	afterID, err := s.repo.LastProcessedMessageID(ctx, peerID)
	if err != nil {
		return RunResult{}, fmt.Errorf("load last processed message id: %w", err)
	}

	candidate, err := s.collectCandidate(ctx, peerID, afterID)
	if err != nil {
		return RunResult{}, fmt.Errorf("collect candidate messages: %w", err)
	}
	if len(candidate.Messages) == 0 {
		return RunResult{Status: RunStatusNoMessages, Trigger: trigger, RequiredCount: s.batchSize}, nil
	}

	result := RunResult{
		Trigger:         trigger,
		FirstMessageID:  candidate.FirstMessageID,
		LastMessageID:   candidate.LastMessageID,
		RangeStart:      candidate.FirstSentAt,
		RangeEnd:        candidate.LastSentAt,
		RawCount:        len(candidate.Messages),
		MeaningfulCount: candidate.MeaningfulCount,
		RequiredCount:   state.NextAttemptMeaningfulCount,
	}

	if trigger == TriggerSourceAuto && candidate.MeaningfulCount < state.NextAttemptMeaningfulCount {
		result.Status = RunStatusNotEnoughMessages
		return result, nil
	}
	if trigger == TriggerSourceManual && candidate.MeaningfulCount == 0 {
		result.Status = RunStatusNoMessages
		return result, nil
	}

	unlock, locked, err := s.repo.AcquireBatchLock(ctx, peerID, candidate.FirstMessageID, candidate.LastMessageID)
	if err != nil {
		return RunResult{}, fmt.Errorf("lock summary batch: %w", err)
	}
	if !locked {
		s.logger.Info("summary batch is locked by another worker",
			slog.Int64("peer_id", peerID),
			slog.Int64("first_message_id", candidate.FirstMessageID),
			slog.Int64("last_message_id", candidate.LastMessageID),
		)
		result.Status = RunStatusLocked
		return result, nil
	}
	defer unlock()

	processed, err := s.repo.IsBatchProcessed(ctx, peerID, candidate.FirstMessageID, candidate.LastMessageID)
	if err != nil {
		return RunResult{}, fmt.Errorf("check processed batch: %w", err)
	}
	if processed {
		result.Status = RunStatusAlreadyProcessed
		return result, nil
	}

	prepared := PrepareMessages(candidate.Messages, s.prepareConfig)
	result.MeaningfulCount = prepared.MeaningfulCount
	if trigger == TriggerSourceAuto && prepared.MeaningfulCount < state.NextAttemptMeaningfulCount {
		result.Status = RunStatusNotEnoughMessages
		return result, nil
	}
	if trigger == TriggerSourceManual && prepared.MeaningfulCount == 0 {
		result.Status = RunStatusNoMessages
		return result, nil
	}

	previousSummary, _, err := s.repo.LastPublishedSummary(ctx, peerID)
	if err != nil {
		return RunResult{}, fmt.Errorf("load previous summary: %w", err)
	}

	prompt := s.promptBuilder.Build(candidate.FirstSentAt, candidate.LastSentAt, previousSummary, prepared, s.maxOutput)
	llmOutput, err := s.llmClient.GenerateSummary(ctx, prompt)
	if err != nil {
		if llm.IsRateLimited(err) {
			nextAttempt := prepared.MeaningfulCount + s.batchSize
			now := time.Now().UTC()
			if state.LastRateLimitNoticeAt == nil || now.Sub(*state.LastRateLimitNoticeAt) >= time.Hour {
				notice := fmt.Sprintf("Уперлись в лимит LLM на этот час. Контекст не потерян: бот попробует снова, когда в этой конфе накопится еще %d осмысленных сообщений.", s.batchSize)
				if publishErr := s.publisher.Publish(ctx, peerID, notice); publishErr != nil {
					s.logger.Warn("failed to publish rate limit notice",
						slog.Int64("peer_id", peerID),
						slog.String("error", publishErr.Error()),
					)
				}
			}
			if err := s.repo.AdvanceSummaryChatRateLimit(ctx, chatID, peerID, nextAttempt, now); err != nil {
				return RunResult{}, fmt.Errorf("persist rate limit summary state: %w", err)
			}
			return RunResult{
				Status:          RunStatusRateLimited,
				Trigger:         trigger,
				FirstMessageID:  candidate.FirstMessageID,
				LastMessageID:   candidate.LastMessageID,
				RangeStart:      candidate.FirstSentAt,
				RangeEnd:        candidate.LastSentAt,
				RawCount:        len(candidate.Messages),
				MeaningfulCount: prepared.MeaningfulCount,
				RequiredCount:   nextAttempt,
			}, nil
		}
		return RunResult{}, fmt.Errorf("generate summary: %w", err)
	}

	summaryText := strings.TrimSpace(llmOutput.Text)
	if summaryText == "" {
		return RunResult{}, fmt.Errorf("llm returned empty summary")
	}
	summaryText = finalizeSummaryText(summaryText)
	issueNumber, err := s.repo.ReserveSummaryIssueNumber(ctx, peerID)
	if err != nil {
		return RunResult{}, fmt.Errorf("reserve summary issue number: %w", err)
	}
	formatData := buildBoldNameFormatData(summaryText, prepared.Messages)
	if err := s.publishSummary(ctx, peerID, summaryText, formatData); err != nil {
		return RunResult{}, fmt.Errorf("publish summary: %w", err)
	}

	if err := s.repo.MarkBatchPublished(ctx, storage.PublishedSummaryBatch{
		ChatID:                 chatID,
		PeerID:                 peerID,
		FirstMessageID:         candidate.FirstMessageID,
		LastMessageID:          candidate.LastMessageID,
		FirstSentAt:            candidate.FirstSentAt,
		LastSentAt:             candidate.LastSentAt,
		RawMessageCount:        len(candidate.Messages),
		MeaningfulMessageCount: prepared.MeaningfulCount,
		SummaryText:            summaryText,
		IssueNumber:            issueNumber,
		LLMProvider:            s.llmClient.Provider(),
		TriggerSource:          string(trigger),
		PublishedAt:            time.Now().UTC(),
	}); err != nil {
		return RunResult{}, fmt.Errorf("persist published summary batch: %w", err)
	}
	if err := s.repo.ResetSummaryChatState(ctx, chatID, peerID, s.batchSize); err != nil {
		return RunResult{}, fmt.Errorf("reset summary chat state: %w", err)
	}

	s.logger.Info("summary published and source messages forgotten",
		slog.Int64("chat_id", chatID),
		slog.Int64("peer_id", peerID),
		slog.String("trigger", string(trigger)),
		slog.Int64("first_message_id", candidate.FirstMessageID),
		slog.Int64("last_message_id", candidate.LastMessageID),
		slog.Int("raw_messages", len(candidate.Messages)),
		slog.Int("meaningful_messages", prepared.MeaningfulCount),
		slog.Int("dropped_messages", prepared.DroppedCount),
		slog.Int64("issue_number", issueNumber),
	)

	result.Status = RunStatusPublished
	result.SummaryText = summaryText
	return result, nil
}

func (s *Service) publishSummary(ctx context.Context, peerID int64, summaryText string, formatData string) error {
	if s.imageGen == nil {
		return s.publisher.PublishFormatted(ctx, peerID, summaryText, formatData)
	}

	imagePrompt := s.buildSummaryImagePrompt(ctx, peerID, summaryText)
	imageBytes, err := s.imageGen.GenerateSummaryImage(ctx, imagePrompt)
	if err != nil {
		s.logger.Warn("failed to generate summary image",
			slog.Int64("peer_id", peerID),
			slog.String("error", err.Error()),
		)
		return s.publisher.PublishFormatted(ctx, peerID, summaryText, formatData)
	}
	if len(imageBytes) == 0 {
		return s.publisher.PublishFormatted(ctx, peerID, summaryText, formatData)
	}

	if err := s.publisher.PublishFormattedWithImage(ctx, peerID, summaryText, formatData, imageBytes); err != nil {
		s.logger.Warn("failed to publish summary image, falling back to text",
			slog.Int64("peer_id", peerID),
			slog.String("error", err.Error()),
		)
		return s.publisher.PublishFormatted(ctx, peerID, summaryText, formatData)
	}
	return nil
}

type candidateBatch struct {
	Messages        []storage.Message
	FirstMessageID  int64
	LastMessageID   int64
	FirstSentAt     time.Time
	LastSentAt      time.Time
	MeaningfulCount int
}

func (s *Service) collectCandidate(ctx context.Context, peerID, afterID int64) (candidateBatch, error) {
	messages := make([]storage.Message, 0, s.fetchLimit)
	lastID := afterID
	meaningfulCount := 0

	for {
		chunk, err := s.repo.ListMessagesAfterID(ctx, peerID, lastID, s.fetchLimit)
		if err != nil {
			return candidateBatch{}, err
		}
		if len(chunk) == 0 {
			break
		}

		for _, message := range chunk {
			messages = append(messages, message)
			lastID = message.ID
			if message.IsOutgoing {
				continue
			}
			if _, ok := MeaningfulText(message.Text, s.prepareConfig.MinMessageLength); ok {
				meaningfulCount++
			}
		}
		if len(chunk) < s.fetchLimit {
			break
		}
	}

	return buildCandidate(messages, meaningfulCount), nil
}

func buildCandidate(messages []storage.Message, meaningfulCount int) candidateBatch {
	if len(messages) == 0 {
		return candidateBatch{}
	}
	first := messages[0]
	last := messages[len(messages)-1]
	return candidateBatch{
		Messages:        messages,
		FirstMessageID:  first.ID,
		LastMessageID:   last.ID,
		FirstSentAt:     first.SentAt.UTC(),
		LastSentAt:      last.SentAt.UTC(),
		MeaningfulCount: meaningfulCount,
	}
}
