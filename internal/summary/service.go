package summary

import (
	"context"
	"fmt"
	"hash/fnv"
	"log/slog"
	"strings"
	"time"

	"bot-summary-vk/internal/config"
	"bot-summary-vk/internal/llm"
	"bot-summary-vk/internal/storage"
	"bot-summary-vk/internal/usage"
)

type Publisher interface {
	Publish(ctx context.Context, peerID int64, text string) error
	PublishFormatted(ctx context.Context, peerID int64, text string, formatData string) error
	PublishFormattedWithRandomID(ctx context.Context, peerID int64, text string, formatData string, randomID int) error
	PublishFormattedWithImage(ctx context.Context, peerID int64, text string, formatData string, image []byte) error
	PublishFormattedWithImageRandomID(ctx context.Context, peerID int64, text string, formatData string, image []byte, randomID int) error
}

type ImageGenerator interface {
	GenerateSummaryImage(ctx context.Context, summaryText string) ([]byte, usage.ImageGenerationUsage, error)
}

type TriggerSource string

const (
	TriggerSourceAuto   TriggerSource = "auto_batch"
	TriggerSourceManual TriggerSource = "manual_command"
)

const previousSummaryLimit = 5

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

type imagePublishStats struct {
	PromptLLMProvider           string
	PromptLLMModel              string
	PromptLLMPromptTokens       int
	PromptLLMCachedPromptTokens int
	PromptLLMCompletionTokens   int
	PromptLLMLatencyMs          int64
	ImageProvider               string
	ImageModel                  string
	ImageInputTokens            int
	ImageInputTextTokens        int
	ImageInputImageTokens       int
	ImageOutputTokens           int
	ImageLatencyMs              int64
	ImagePublished              bool
}

type Service struct {
	repo                 *storage.Repository
	llmClient            llm.Client
	imagePromptLLMClient llm.Client
	llmModel             string
	imagePromptLLMModel  string
	publisher            Publisher
	prepareConfig        PrepareConfig
	promptBuilder        PromptBuilder
	logger               *slog.Logger
	imageGen             ImageGenerator
	batchSize            int
	fetchLimit           int
	maxOutput            int
}

func NewService(repo *storage.Repository, llmClient llm.Client, imagePromptLLMClient llm.Client, publisher Publisher, cfg config.Config, logger *slog.Logger, imageGen ImageGenerator) *Service {
	fetchLimit := cfg.Summary.BatchSize * 3
	if fetchLimit < 500 {
		fetchLimit = 500
	}

	return &Service{
		repo:                 repo,
		llmClient:            llmClient,
		imagePromptLLMClient: imagePromptLLMClient,
		llmModel:             cfg.LLM.Model,
		imagePromptLLMModel:  cfg.ImagePromptLLM.Model,
		publisher:            publisher,
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

	result := runResultFromCandidate(trigger, candidate, state.NextAttemptMeaningfulCount)
	if status, skip := skipForMessageCount(trigger, candidate.MeaningfulCount, state.NextAttemptMeaningfulCount); skip {
		result.Status = status
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
	if status, skip := skipForMessageCount(trigger, prepared.MeaningfulCount, state.NextAttemptMeaningfulCount); skip {
		result.Status = status
		return result, nil
	}

	llmOutput, summaryText, err := s.generateSummaryText(ctx, peerID, candidate, prepared)
	if err != nil {
		if llm.IsRateLimited(err) {
			return s.handleRateLimit(ctx, chatID, peerID, trigger, state, candidate, prepared)
		}
		return RunResult{}, fmt.Errorf("generate summary: %w", err)
	}

	publishText := appendSummaryHashtags(summaryText)
	issueNumber, err := s.repo.ReserveSummaryIssueNumber(ctx, peerID)
	if err != nil {
		return RunResult{}, fmt.Errorf("reserve summary issue number: %w", err)
	}
	formatData := buildBoldNameFormatData(publishText, prepared.Messages)
	randomID := deterministicSummaryRandomID(peerID, candidate.FirstMessageID, candidate.LastMessageID)
	imageStats, err := s.publishWithOptionalImage(ctx, peerID, summaryText, publishText, formatData, randomID)
	if err != nil {
		return RunResult{}, fmt.Errorf("publish summary: %w", err)
	}

	batch := s.buildPublishedBatch(chatID, peerID, candidate, prepared, summaryText, issueNumber, trigger, llmOutput, imageStats)
	if err := s.repo.MarkBatchPublished(ctx, batch); err != nil {
		return RunResult{}, fmt.Errorf("persist published summary batch: %w", err)
	}
	if err := s.repo.ResetSummaryChatState(ctx, chatID, peerID, s.batchSize); err != nil {
		return RunResult{}, fmt.Errorf("reset summary chat state: %w", err)
	}

	s.logPublishedSummary(chatID, peerID, trigger, candidate, prepared, issueNumber, llmOutput, imageStats)
	result.Status = RunStatusPublished
	result.SummaryText = summaryText
	return result, nil
}

func runResultFromCandidate(trigger TriggerSource, candidate candidateBatch, requiredCount int) RunResult {
	return RunResult{
		Trigger:         trigger,
		FirstMessageID:  candidate.FirstMessageID,
		LastMessageID:   candidate.LastMessageID,
		RangeStart:      candidate.FirstSentAt,
		RangeEnd:        candidate.LastSentAt,
		RawCount:        len(candidate.Messages),
		MeaningfulCount: candidate.MeaningfulCount,
		RequiredCount:   requiredCount,
	}
}

func skipForMessageCount(trigger TriggerSource, meaningfulCount, requiredCount int) (RunStatus, bool) {
	if trigger == TriggerSourceAuto && meaningfulCount < requiredCount {
		return RunStatusNotEnoughMessages, true
	}
	if trigger == TriggerSourceManual && meaningfulCount == 0 {
		return RunStatusNoMessages, true
	}
	return "", false
}

func (s *Service) generateSummaryText(ctx context.Context, peerID int64, candidate candidateBatch, prepared PreparedWindow) (llm.GenerateSummaryOutput, string, error) {
	previousSummaries, err := s.repo.LastPublishedSummaries(ctx, peerID, previousSummaryLimit)
	if err != nil {
		return llm.GenerateSummaryOutput{}, "", fmt.Errorf("load previous summaries: %w", err)
	}

	prompt := s.promptBuilder.Build(candidate.FirstSentAt, candidate.LastSentAt, previousSummaries, prepared, s.maxOutput)
	llmOutput, err := s.llmClient.GenerateSummary(ctx, prompt)
	if err != nil {
		return llm.GenerateSummaryOutput{}, "", err
	}
	summaryText, err := normalizeSummaryText(llmOutput.Text)
	if err != nil {
		return llm.GenerateSummaryOutput{}, "", err
	}
	return llmOutput, summaryText, nil
}

func normalizeSummaryText(text string) (string, error) {
	text = strings.TrimSpace(text)
	if text == "" {
		return "", fmt.Errorf("llm returned empty summary")
	}
	text = finalizeSummaryText(text)
	if text == "" {
		return "", fmt.Errorf("llm returned empty summary after cleanup")
	}
	return text, nil
}

func (s *Service) handleRateLimit(ctx context.Context, chatID, peerID int64, trigger TriggerSource, state storage.SummaryChatState, candidate candidateBatch, prepared PreparedWindow) (RunResult, error) {
	nextAttempt := prepared.MeaningfulCount + s.batchSize
	now := time.Now().UTC()
	if state.LastRateLimitNoticeAt == nil || now.Sub(*state.LastRateLimitNoticeAt) >= time.Hour {
		notice := fmt.Sprintf("Редактор сорвал голос на прошлом выпуске и объявил технический перекур. Контекст не потерян: бот попробует снова, когда в этой конфе накопится еще %d осмысленных сообщений.", s.batchSize)
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

	result := runResultFromCandidate(trigger, candidate, nextAttempt)
	result.Status = RunStatusRateLimited
	result.MeaningfulCount = prepared.MeaningfulCount
	return result, nil
}

func (s *Service) buildPublishedBatch(chatID, peerID int64, candidate candidateBatch, prepared PreparedWindow, summaryText string, issueNumber int64, trigger TriggerSource, llmOutput llm.GenerateSummaryOutput, imageStats imagePublishStats) storage.PublishedSummaryBatch {
	return storage.PublishedSummaryBatch{
		ChatID:                           chatID,
		PeerID:                           peerID,
		FirstMessageID:                   candidate.FirstMessageID,
		LastMessageID:                    candidate.LastMessageID,
		FirstSentAt:                      candidate.FirstSentAt,
		LastSentAt:                       candidate.LastSentAt,
		RawMessageCount:                  len(candidate.Messages),
		MeaningfulMessageCount:           prepared.MeaningfulCount,
		SummaryText:                      summaryText,
		IssueNumber:                      issueNumber,
		LLMProvider:                      s.llmClient.Provider(),
		LLMModel:                         s.llmModel,
		LLMPromptTokens:                  llmOutput.PromptTokens,
		LLMCachedPromptTokens:            llmOutput.CachedPromptTokens,
		LLMCompletionTokens:              llmOutput.CompletionTokens,
		LLMLatencyMs:                     llmOutput.Duration.Milliseconds(),
		ImagePromptLLMProvider:           imageStats.PromptLLMProvider,
		ImagePromptLLMModel:              imageStats.PromptLLMModel,
		ImagePromptLLMPromptTokens:       imageStats.PromptLLMPromptTokens,
		ImagePromptLLMCachedPromptTokens: imageStats.PromptLLMCachedPromptTokens,
		ImagePromptLLMCompletionTokens:   imageStats.PromptLLMCompletionTokens,
		ImagePromptLLMLatencyMs:          imageStats.PromptLLMLatencyMs,
		ImageProvider:                    imageStats.ImageProvider,
		ImageModel:                       imageStats.ImageModel,
		ImageInputTokens:                 imageStats.ImageInputTokens,
		ImageInputTextTokens:             imageStats.ImageInputTextTokens,
		ImageInputImageTokens:            imageStats.ImageInputImageTokens,
		ImageOutputTokens:                imageStats.ImageOutputTokens,
		ImageLatencyMs:                   imageStats.ImageLatencyMs,
		ImagePublished:                   imageStats.ImagePublished,
		TriggerSource:                    string(trigger),
		PublishedAt:                      time.Now().UTC(),
	}
}

func (s *Service) logPublishedSummary(chatID, peerID int64, trigger TriggerSource, candidate candidateBatch, prepared PreparedWindow, issueNumber int64, llmOutput llm.GenerateSummaryOutput, imageStats imagePublishStats) {
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
		slog.Int("llm_prompt_tokens", llmOutput.PromptTokens),
		slog.Int("llm_cached_prompt_tokens", llmOutput.CachedPromptTokens),
		slog.Int("llm_completion_tokens", llmOutput.CompletionTokens),
		slog.Duration("llm_latency", llmOutput.Duration),
		slog.Int("image_prompt_llm_prompt_tokens", imageStats.PromptLLMPromptTokens),
		slog.Int("image_prompt_llm_completion_tokens", imageStats.PromptLLMCompletionTokens),
		slog.Int("image_input_tokens", imageStats.ImageInputTokens),
		slog.Int("image_output_tokens", imageStats.ImageOutputTokens),
		slog.Bool("image_published", imageStats.ImagePublished),
	)
}

func (s *Service) publishWithOptionalImage(ctx context.Context, peerID int64, summaryText string, publishText string, formatData string, randomID int) (imagePublishStats, error) {
	if s.imageGen == nil {
		return imagePublishStats{}, s.publisher.PublishFormattedWithRandomID(ctx, peerID, publishText, formatData, randomID)
	}

	imagePrompt, promptOutput := s.buildSummaryImagePrompt(ctx, peerID, summaryText)
	stats := imagePublishStats{}
	if promptOutput.Duration > 0 || promptOutput.PromptTokens > 0 || promptOutput.CompletionTokens > 0 {
		stats.PromptLLMProvider = s.imagePromptProvider()
		stats.PromptLLMModel = s.imagePromptLLMModel
		stats.PromptLLMPromptTokens = promptOutput.PromptTokens
		stats.PromptLLMCachedPromptTokens = promptOutput.CachedPromptTokens
		stats.PromptLLMCompletionTokens = promptOutput.CompletionTokens
		stats.PromptLLMLatencyMs = promptOutput.Duration.Milliseconds()
	}

	imageBytes, imageUsage, err := s.imageGen.GenerateSummaryImage(ctx, imagePrompt)
	stats.ImageProvider = imageUsage.Provider
	stats.ImageModel = imageUsage.Model
	stats.ImageInputTokens = imageUsage.InputTokens
	stats.ImageInputTextTokens = imageUsage.InputTextTokens
	stats.ImageInputImageTokens = imageUsage.InputImageTokens
	stats.ImageOutputTokens = imageUsage.OutputTokens
	stats.ImageLatencyMs = imageUsage.Duration.Milliseconds()
	if err != nil {
		s.logger.Warn("failed to generate summary image",
			slog.Int64("peer_id", peerID),
			slog.String("error", err.Error()),
		)
		return stats, s.publisher.PublishFormattedWithRandomID(ctx, peerID, publishText, formatData, randomID)
	}
	if len(imageBytes) == 0 {
		return stats, s.publisher.PublishFormattedWithRandomID(ctx, peerID, publishText, formatData, randomID)
	}

	if err := s.publisher.PublishFormattedWithImageRandomID(ctx, peerID, publishText, formatData, imageBytes, randomID); err != nil {
		s.logger.Warn("failed to publish summary image, falling back to text",
			slog.Int64("peer_id", peerID),
			slog.String("error", err.Error()),
		)
		return stats, s.publisher.PublishFormattedWithRandomID(ctx, peerID, publishText, formatData, randomID)
	}
	stats.ImagePublished = true
	return stats, nil
}

func (s *Service) imagePromptProvider() string {
	if s.imagePromptLLMClient == nil {
		return s.llmClient.Provider()
	}
	return s.imagePromptLLMClient.Provider()
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

func deterministicSummaryRandomID(peerID, firstMessageID, lastMessageID int64) int {
	h := fnv.New32a()
	_, _ = h.Write([]byte(fmt.Sprintf("summary:%d:%d:%d", peerID, firstMessageID, lastMessageID)))
	id := int(h.Sum32() & 0x7fffffff)
	if id == 0 {
		return 1
	}
	return id
}
