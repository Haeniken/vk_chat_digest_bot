package app

import (
	"context"
	"fmt"
	"log/slog"
	"strings"
	"time"

	"bot-summary-vk/internal/config"
	"bot-summary-vk/internal/storage"
	"bot-summary-vk/internal/summary"
	"bot-summary-vk/internal/vk"
)

type MessageIngestionService struct {
	repo                   *storage.Repository
	logger                 *slog.Logger
	manual                 config.ManualTriggerConfig
	publisher              summary.Publisher
	summary                *summary.Service
	resolver               senderNameResolver
	manualExecutionTimeout time.Duration
}

type senderNameResolver interface {
	ResolveSenderName(ctx context.Context, senderID int64) (string, error)
}

func NewMessageIngestionService(
	repo *storage.Repository,
	manual config.ManualTriggerConfig,
	publisher summary.Publisher,
	summaryService *summary.Service,
	resolver senderNameResolver,
	manualExecutionTimeout time.Duration,
	logger *slog.Logger,
) *MessageIngestionService {
	if manualExecutionTimeout <= 0 {
		manualExecutionTimeout = 12 * time.Minute
	}
	return &MessageIngestionService{
		repo:                   repo,
		logger:                 logger,
		manual:                 manual,
		publisher:              publisher,
		summary:                summaryService,
		resolver:               resolver,
		manualExecutionTimeout: manualExecutionTimeout,
	}
}

func (s *MessageIngestionService) HandleMessage(ctx context.Context, message vk.IncomingMessage) error {
	if !isChatMessage(message.PeerID) {
		return nil
	}
	if message.SenderID < 0 {
		s.logger.Debug("skip message from community sender",
			slog.Int64("peer_id", message.PeerID),
			slog.Int64("sender_id", message.SenderID),
		)
		return nil
	}

	senderName := ""
	if s.resolver != nil {
		resolvedName, err := s.resolver.ResolveSenderName(ctx, message.SenderID)
		if err != nil {
			s.logger.Warn("failed to resolve sender name",
				slog.Int64("sender_id", message.SenderID),
				slog.String("error", err.Error()),
			)
		} else {
			senderName = resolvedName
		}
	}

	replyToSenderName := ""
	if message.ReplyToSenderID > 0 {
		if message.ReplyToSenderID == message.SenderID {
			replyToSenderName = senderName
		} else if s.resolver != nil {
			resolvedName, err := s.resolver.ResolveSenderName(ctx, message.ReplyToSenderID)
			if err != nil {
				s.logger.Warn("failed to resolve reply sender name",
					slog.Int64("sender_id", message.ReplyToSenderID),
					slog.String("error", err.Error()),
				)
			} else {
				replyToSenderName = resolvedName
			}
		}
	}

	if err := s.repo.SaveMessage(ctx, storage.Message{
		SourceMessageID:              message.SourceMessageID,
		ConversationMessageID:        message.ConversationMessageID,
		ChatID:                       message.ChatID,
		PeerID:                       message.PeerID,
		SenderID:                     message.SenderID,
		SenderName:                   senderName,
		Text:                         message.Text,
		ReplyToSourceMessageID:       message.ReplyToSourceMessageID,
		ReplyToConversationMessageID: message.ReplyToConversationMessageID,
		ReplyToSenderID:              message.ReplyToSenderID,
		ReplyToSenderName:            replyToSenderName,
		ReplyToText:                  compactReplyText(message.ReplyToText, 500),
		SentAt:                       message.SentAt,
		ReceivedAt:                   time.Now().UTC(),
		IsOutgoing:                   message.IsOutgoing,
	}); err != nil {
		return fmt.Errorf("persist incoming message: %w", err)
	}

	s.logger.Debug("message persisted", slog.Int64("peer_id", message.PeerID), slog.Int64("message_id", message.SourceMessageID), slog.Int64("sender_id", message.SenderID))

	if err := s.handleManualTrigger(ctx, message); err != nil {
		return fmt.Errorf("handle manual trigger: %w", err)
	}
	if err := s.handleAutoSummary(ctx, message); err != nil {
		return fmt.Errorf("handle automatic summary: %w", err)
	}

	return nil
}

func (s *MessageIngestionService) handleManualTrigger(ctx context.Context, message vk.IncomingMessage) error {
	if s.summary == nil || s.publisher == nil {
		return nil
	}
	if !matchesTrigger(message.Text, s.manual.Command) {
		return nil
	}
	if !s.isManualSenderAllowed(message.SenderID) {
		s.logger.Info("manual trigger rejected: sender is not allowed",
			slog.Int64("sender_id", message.SenderID),
			slog.Int64("peer_id", message.PeerID),
			slog.String("command", strings.TrimSpace(message.Text)),
		)
		return nil
	}
	s.logger.Info("manual trigger accepted",
		slog.Int64("sender_id", message.SenderID),
		slog.Int64("peer_id", message.PeerID),
		slog.Int64("chat_id", message.ChatID),
	)

	go s.runManualSummary(message.ChatID, message.PeerID, message.SenderID)
	return nil
}

func (s *MessageIngestionService) runManualSummary(chatID, peerID, senderID int64) {
	ctx, cancel := context.WithTimeout(context.Background(), s.manualExecutionTimeout)
	defer cancel()

	result, err := s.summary.ExecuteManual(ctx, chatID, peerID)
	if err != nil {
		s.logger.Error("manual summary trigger failed",
			slog.Int64("sender_id", senderID),
			slog.Int64("peer_id", peerID),
			slog.String("error", err.Error()),
		)
		if publishErr := s.publisher.Publish(ctx, peerID, "Не смог собрать summary. Подробности уже в логах."); publishErr != nil {
			s.logger.Warn("failed to publish manual summary failure notice",
				slog.Int64("peer_id", peerID),
				slog.String("error", publishErr.Error()),
			)
		}
		return
	}
	s.logger.Info("manual summary run completed",
		slog.Int64("sender_id", senderID),
		slog.Int64("peer_id", peerID),
		slog.String("status", string(result.Status)),
		slog.Int("meaningful_count", result.MeaningfulCount),
		slog.Int("required_count", result.RequiredCount),
	)

	if err := s.publishManualResult(ctx, peerID, result); err != nil {
		s.logger.Warn("failed to publish manual summary result",
			slog.Int64("peer_id", peerID),
			slog.String("status", string(result.Status)),
			slog.String("error", err.Error()),
		)
	}
}

func (s *MessageIngestionService) publishManualResult(ctx context.Context, peerID int64, result summary.RunResult) error {
	switch result.Status {
	case summary.RunStatusPublished:
		return nil
	case summary.RunStatusAlreadyProcessed:
		return s.publisher.Publish(ctx, peerID, fmt.Sprintf("Summary для диапазона сообщений %d-%d уже был опубликован.", result.FirstMessageID, result.LastMessageID))
	case summary.RunStatusLocked:
		return s.publisher.Publish(ctx, peerID, "Summary уже собирается другим процессом. Попробуй через минуту.")
	case summary.RunStatusNotEnoughMessages:
		return s.publisher.Publish(ctx, peerID, fmt.Sprintf("Не публикую summary: накопилось только %d осмысленных сообщений, а для авто-публикации нужно %d.", result.MeaningfulCount, result.RequiredCount))
	case summary.RunStatusNoMessages:
		return s.publisher.Publish(ctx, peerID, "После прошлого summary новых осмысленных сообщений пока не накопилось.")
	case summary.RunStatusRateLimited:
		return s.publisher.Publish(ctx, peerID, fmt.Sprintf("Уперлись в почасовой лимит LLM. Контекст сохранен, следующая автопопытка будет после того, как в этой конфе накопится %d осмысленных сообщений.", result.RequiredCount))
	default:
		return s.publisher.Publish(ctx, peerID, "Команда принята, но результат оказался неожиданным. Проверь логи.")
	}
}

func matchesTrigger(text, command string) bool {
	return strings.EqualFold(strings.TrimSpace(text), strings.TrimSpace(command))
}

func compactReplyText(text string, maxRunes int) string {
	text = strings.ReplaceAll(text, "\n", " ")
	text = strings.Join(strings.Fields(text), " ")
	if maxRunes <= 0 {
		return ""
	}
	runes := []rune(text)
	if len(runes) <= maxRunes {
		return text
	}
	if maxRunes <= 3 {
		return string(runes[:maxRunes])
	}
	return string(runes[:maxRunes-3]) + "..."
}

func (s *MessageIngestionService) isManualSenderAllowed(senderID int64) bool {
	if senderID <= 0 {
		return false
	}
	_, ok := s.manual.UserIDsSet[senderID]
	return ok
}

func (s *MessageIngestionService) handleAutoSummary(ctx context.Context, message vk.IncomingMessage) error {
	if s.summary == nil {
		return nil
	}

	for {
		result, err := s.summary.ExecuteAuto(ctx, message.ChatID, message.PeerID)
		if err != nil {
			return err
		}

		switch result.Status {
		case summary.RunStatusPublished:
			continue
		case summary.RunStatusLocked, summary.RunStatusAlreadyProcessed, summary.RunStatusNotEnoughMessages, summary.RunStatusNoMessages, summary.RunStatusRateLimited:
			return nil
		default:
			s.logger.Warn("automatic summary returned unexpected status", slog.String("status", string(result.Status)))
			return nil
		}
	}
}

func isChatMessage(peerID int64) bool {
	const peerOffset int64 = 2_000_000_000
	return peerID > peerOffset
}
