package app

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"strings"
	"sync"
	"time"

	"bot-summary-vk/internal/config"
	"bot-summary-vk/internal/llm"
	"bot-summary-vk/internal/storage"
	"bot-summary-vk/internal/summary"
	"bot-summary-vk/internal/vk"
)

type MessageIngestionService struct {
	repo                   *storage.Repository
	logger                 *slog.Logger
	manual                 config.ManualTriggerConfig
	publisher              summary.Publisher
	pinger                 vkPinger
	summary                *summary.Service
	llmModel               string
	imagePromptLLMModel    string
	imageModel             string
	resolver               senderNameResolver
	manualExecutionTimeout time.Duration
	lifecycleMu            sync.Mutex
	lifecycleCtx           context.Context
	lifecycleCancel        context.CancelFunc
	jobsWg                 sync.WaitGroup
	autoMu                 sync.Mutex
	autoRuns               map[int64]*autoSummaryRunState
}

type autoSummaryRunState struct {
	running bool
	pending bool
}

type senderNameResolver interface {
	ResolveSenderName(ctx context.Context, senderID int64) (string, error)
}

type vkPinger interface {
	Ping(ctx context.Context) (time.Duration, error)
}

func NewMessageIngestionService(
	repo *storage.Repository,
	manual config.ManualTriggerConfig,
	publisher summary.Publisher,
	pinger vkPinger,
	summaryService *summary.Service,
	llmModel string,
	imagePromptLLMModel string,
	imageModel string,
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
		pinger:                 pinger,
		summary:                summaryService,
		llmModel:               llmModel,
		imagePromptLLMModel:    imagePromptLLMModel,
		imageModel:             imageModel,
		resolver:               resolver,
		manualExecutionTimeout: manualExecutionTimeout,
		autoRuns:               make(map[int64]*autoSummaryRunState),
	}
}

func (s *MessageIngestionService) Start(ctx context.Context) {
	if ctx == nil {
		ctx = context.Background()
	}

	s.lifecycleMu.Lock()
	if s.lifecycleCancel != nil {
		s.lifecycleCancel()
	}
	s.lifecycleCtx, s.lifecycleCancel = context.WithCancel(ctx)
	s.lifecycleMu.Unlock()
}

func (s *MessageIngestionService) StopAndWait() {
	s.lifecycleMu.Lock()
	cancel := s.lifecycleCancel
	s.lifecycleCtx = nil
	s.lifecycleCancel = nil
	if cancel != nil {
		cancel()
	}
	s.lifecycleMu.Unlock()

	s.jobsWg.Wait()
}

func (s *MessageIngestionService) startBackgroundJob(name string, peerID int64, job func(context.Context)) bool {
	s.lifecycleMu.Lock()
	ctx := s.lifecycleCtx
	if ctx == nil || ctx.Err() != nil {
		s.lifecycleMu.Unlock()
		s.logger.Warn("background summary job rejected: app is shutting down",
			slog.String("job", name),
			slog.Int64("peer_id", peerID),
		)
		return false
	}
	s.jobsWg.Add(1)
	s.lifecycleMu.Unlock()

	go func() {
		defer s.jobsWg.Done()
		job(ctx)
	}()
	return true
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
	if isAvailabilityPing(message.Text) {
		if s.publisher == nil {
			return nil
		}
		if err := s.publisher.Publish(ctx, message.PeerID, "ПОНГ"); err != nil {
			return fmt.Errorf("publish ping response: %w", err)
		}
		s.logger.Debug("availability ping answered",
			slog.Int64("peer_id", message.PeerID),
			slog.Int64("sender_id", message.SenderID),
		)
		return nil
	}
	if isEasterEggHare(message.Text) {
		if s.publisher == nil {
			return nil
		}
		if err := s.publisher.Publish(ctx, message.PeerID, "ПЕТУХ"); err != nil {
			return fmt.Errorf("publish hare easter egg response: %w", err)
		}
		s.logger.Debug("hare easter egg answered",
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
	s.scheduleAutoSummary(message.ChatID, message.PeerID)

	return nil
}

func (s *MessageIngestionService) HandleMessageEvent(ctx context.Context, event vk.MessageEvent) error {
	if !isChatMessage(event.PeerID) {
		return nil
	}
	command := debugPayloadCommand(event.Payload)
	if command == "" {
		return nil
	}
	if !s.isManualSenderAllowed(event.UserID) {
		s.answerDebugEvent(ctx, event, randomDebugUnauthorizedPhrase())
		s.logger.Info("debug callback rejected: sender is not allowed",
			slog.Int64("sender_id", event.UserID),
			slog.Int64("peer_id", event.PeerID),
		)
		return nil
	}
	if event.ConversationMessageID <= 0 {
		s.answerDebugEvent(ctx, event, "VK не прислал номер сообщения, редактировать нечего.")
		s.logger.Warn("debug callback has no conversation message id",
			slog.Int64("sender_id", event.UserID),
			slog.Int64("peer_id", event.PeerID),
		)
		return nil
	}

	show7Days := command == debugDetailsPayloadCommand
	if err := s.publishDebugUsage(ctx, event.PeerID, event.ConversationMessageID, show7Days); err != nil {
		s.answerDebugEvent(ctx, event, "VK не дал перешить эту газетную полосу.")
		s.logger.Warn("failed to edit debug usage message",
			slog.Int64("sender_id", event.UserID),
			slog.Int64("peer_id", event.PeerID),
			slog.Int64("conversation_message_id", event.ConversationMessageID),
			slog.String("command", command),
			slog.String("error", err.Error()),
		)
		return nil
	}
	if show7Days {
		s.answerDebugEvent(ctx, event, "Показываю расширенную статистику.")
	} else {
		s.answerDebugEvent(ctx, event, "Свернул до месячных расходов.")
	}
	return nil
}

func (s *MessageIngestionService) answerDebugEvent(ctx context.Context, event vk.MessageEvent, text string) {
	responder, ok := s.publisher.(debugEventResponder)
	if !ok {
		return
	}
	if err := responder.AnswerMessageEvent(ctx, event.EventID, event.UserID, event.PeerID, text); err != nil {
		s.logger.Warn("failed to answer debug callback",
			slog.Int64("sender_id", event.UserID),
			slog.Int64("peer_id", event.PeerID),
			slog.String("error", err.Error()),
		)
	}
}

func (s *MessageIngestionService) handleManualTrigger(ctx context.Context, message vk.IncomingMessage) error {
	if s.publisher == nil {
		return nil
	}
	if strings.TrimSpace(s.manual.DebugCommand) != "" && matchesTrigger(message.Text, s.manual.DebugCommand) {
		return s.handleDebugCommand(ctx, message, false)
	}
	if s.summary == nil || !matchesTrigger(message.Text, s.manual.Command) {
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

	if !s.startBackgroundJob("manual_summary", message.PeerID, func(jobCtx context.Context) {
		s.runManualSummary(jobCtx, message.ChatID, message.PeerID, message.SenderID)
	}) {
		return s.publisher.Publish(ctx, message.PeerID, "Редакция уже гасит свет и закрывает выпускной стол. Дергать новую сводку лучше после перезапуска.")
	}
	return nil
}

func (s *MessageIngestionService) runManualSummary(parentCtx context.Context, chatID, peerID, senderID int64) {
	ctx, cancel := context.WithTimeout(parentCtx, s.manualExecutionTimeout)
	defer cancel()

	result, err := s.summary.ExecuteManual(ctx, chatID, peerID)
	if err != nil {
		s.logger.Error("manual summary trigger failed",
			slog.Int64("sender_id", senderID),
			slog.Int64("peer_id", peerID),
			slog.String("error", err.Error()),
		)
		if parentCtx.Err() != nil {
			return
		}
		publishCtx, publishCancel := context.WithTimeout(parentCtx, 10*time.Second)
		defer publishCancel()
		if publishErr := s.publisher.Publish(publishCtx, peerID, manualSummaryFailureMessage(err)); publishErr != nil {
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

	if parentCtx.Err() != nil {
		return
	}
	publishCtx, publishCancel := context.WithTimeout(parentCtx, 10*time.Second)
	defer publishCancel()
	if err := s.publishManualResult(publishCtx, peerID, result); err != nil {
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
		return s.publisher.Publish(ctx, peerID, fmt.Sprintf("Этот выпуск уже ушел в печать: диапазон %d-%d повторно не гоняем, редакция не любит дубликаты.", result.FirstMessageID, result.LastMessageID))
	case summary.RunStatusLocked:
		return s.publisher.Publish(ctx, peerID, "Редактор уже заперся в кабинете и собирает выпуск. Дайте ему минуту, пока он не начал спорить с мебелью.")
	case summary.RunStatusNotEnoughMessages:
		return s.publisher.Publish(ctx, peerID, fmt.Sprintf("Редакция открыла папку, а там всего %d осмысленных реплик из нужных %d. Для полноценного скандала пока маловато дыма.", result.MeaningfulCount, result.RequiredCount))
	case summary.RunStatusNoMessages:
		return s.publisher.Publish(ctx, peerID, "После прошлого выпуска в редакционную урну не прилетело ничего, из чего можно сварить новую драму.")
	case summary.RunStatusRateLimited:
		remaining := result.RequiredCount - result.MeaningfulCount
		if remaining < 0 {
			remaining = 0
		}
		return s.publisher.Publish(ctx, peerID, fmt.Sprintf("Редактор сорвал голос на прошлом выпуске и объявил технический перекур. Контекст не потерян: следующая автопопытка будет после еще %d осмысленных сообщений.", remaining))
	default:
		return s.publisher.Publish(ctx, peerID, "Команда принята, но в редакции случился странный хлопок. Подробности уже в логах.")
	}
}

func matchesTrigger(text, command string) bool {
	return strings.EqualFold(strings.TrimSpace(text), strings.TrimSpace(command))
}

func isAvailabilityPing(text string) bool {
	return strings.EqualFold(strings.TrimSpace(text), "пинг")
}

func isEasterEggHare(text string) bool {
	return strings.EqualFold(strings.TrimSpace(text), "заяц")
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

func (s *MessageIngestionService) scheduleAutoSummary(chatID, peerID int64) {
	if s.summary == nil {
		return
	}

	s.autoMu.Lock()
	state := s.autoRuns[peerID]
	if state != nil && state.running {
		state.pending = true
		s.autoMu.Unlock()
		return
	}
	s.autoRuns[peerID] = &autoSummaryRunState{running: true}
	s.autoMu.Unlock()

	if !s.startBackgroundJob("auto_summary", peerID, func(jobCtx context.Context) {
		s.runAutoSummary(jobCtx, chatID, peerID)
	}) {
		s.autoMu.Lock()
		delete(s.autoRuns, peerID)
		s.autoMu.Unlock()
	}
}

func (s *MessageIngestionService) runAutoSummary(parentCtx context.Context, chatID, peerID int64) {
	defer s.clearAutoSummaryRun(peerID)

	for {
		if parentCtx.Err() != nil {
			s.logger.Info("automatic summary stopped by application shutdown",
				slog.Int64("chat_id", chatID),
				slog.Int64("peer_id", peerID),
			)
			return
		}

		ctx, cancel := context.WithTimeout(parentCtx, s.manualExecutionTimeout)
		err := s.handleAutoSummary(ctx, chatID, peerID)
		cancel()
		if err != nil {
			if parentCtx.Err() != nil || errors.Is(err, context.Canceled) {
				s.logger.Info("automatic summary canceled",
					slog.Int64("chat_id", chatID),
					slog.Int64("peer_id", peerID),
				)
				return
			}
			failureCount := s.recordAutoSummaryFailure(parentCtx, peerID)
			s.logger.Error("automatic summary failed",
				slog.Int64("chat_id", chatID),
				slog.Int64("peer_id", peerID),
				slog.Int("failure_count", failureCount),
				slog.Int("max_attempts", summary.MaxAutoSummaryAttempts),
				slog.String("error", err.Error()),
			)
		}

		s.autoMu.Lock()
		state := s.autoRuns[peerID]
		if state == nil || !state.pending {
			s.autoMu.Unlock()
			return
		}
		state.pending = false
		s.autoMu.Unlock()
	}
}

func (s *MessageIngestionService) recordAutoSummaryFailure(ctx context.Context, peerID int64) int {
	recordCtx, cancel := context.WithTimeout(ctx, 5*time.Second)
	defer cancel()

	failureCount, err := s.repo.RecordAutoSummaryFailure(recordCtx, peerID)
	if err != nil {
		s.logger.Warn("failed to record automatic summary failure",
			slog.Int64("peer_id", peerID),
			slog.String("error", err.Error()),
		)
		return 0
	}
	if failureCount == summary.MaxAutoSummaryAttempts {
		s.logger.Error("automatic summary retry limit reached",
			slog.Int64("peer_id", peerID),
			slog.Int("failure_count", failureCount),
			slog.Int("max_attempts", summary.MaxAutoSummaryAttempts),
		)
	}
	return failureCount
}

func (s *MessageIngestionService) clearAutoSummaryRun(peerID int64) {
	s.autoMu.Lock()
	delete(s.autoRuns, peerID)
	s.autoMu.Unlock()
}

func (s *MessageIngestionService) handleAutoSummary(ctx context.Context, chatID, peerID int64) error {
	for {
		result, err := s.summary.ExecuteAuto(ctx, chatID, peerID)
		if err != nil {
			return err
		}

		switch result.Status {
		case summary.RunStatusPublished:
			continue
		case summary.RunStatusLocked, summary.RunStatusAlreadyProcessed, summary.RunStatusNotEnoughMessages, summary.RunStatusNoMessages, summary.RunStatusRateLimited, summary.RunStatusRetryLimitReached:
			return nil
		default:
			s.logger.Warn("automatic summary returned unexpected status", slog.String("status", string(result.Status)))
			return nil
		}
	}
}

func manualSummaryFailureMessage(err error) string {
	if errors.Is(err, context.DeadlineExceeded) || errors.Is(err, context.Canceled) {
		return "Редактор ушел в запой прямо на дедлайне: выпуск слишком долго не собирался. Контекст сохранен, можно дернуть позже."
	}
	var statusErr *llm.HTTPStatusError
	if errors.As(err, &statusErr) {
		return fmt.Sprintf("Редактор забухал, а LLM вместо текста прислала HTTP %d: %s. Материалы не потерялись, можно повторить позже.", statusErr.StatusCode, statusErr.PublicMessage())
	}
	message := strings.ToLower(err.Error())
	switch {
	case strings.Contains(message, "generate summary"):
		return "Редактор забухал, стучит стаканом по батарее и не отдает текст. Контекст жив, можно повторить позже."
	case strings.Contains(message, "publish summary"):
		return "Выпуск собран, но курьер VK уронил пачку в подъезде. Контекст сохранен, можно повторить позже."
	case strings.Contains(message, "collect candidate"), strings.Contains(message, "load previous summary"), strings.Contains(message, "persist"), strings.Contains(message, "reset summary chat state"):
		return "Архивариус уснул лицом в картотеке: хранилище не отдало материалы как надо. Контекст сохранен, подробности уже в логах."
	default:
		return "В редакции короткое замыкание: выпуск сорвался по внутренней причине. Контекст сохранен, подробности уже в логах."
	}
}

func isChatMessage(peerID int64) bool {
	const peerOffset int64 = 2_000_000_000
	return peerID > peerOffset
}
