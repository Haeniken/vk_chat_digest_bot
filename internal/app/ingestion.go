package app

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"math"
	"strings"
	"sync"
	"time"
	"unicode/utf16"

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
	resolver               senderNameResolver
	manualExecutionTimeout time.Duration
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
		resolver:               resolver,
		manualExecutionTimeout: manualExecutionTimeout,
		autoRuns:               make(map[int64]*autoSummaryRunState),
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
	s.scheduleAutoSummary(message.ChatID, message.PeerID)

	return nil
}

func (s *MessageIngestionService) handleManualTrigger(ctx context.Context, message vk.IncomingMessage) error {
	if s.publisher == nil {
		return nil
	}
	if matchesTrigger(message.Text, "/livanda-debug") {
		return s.handleDebugCommand(ctx, message)
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
		if publishErr := s.publisher.Publish(ctx, peerID, manualSummaryFailureMessage(err)); publishErr != nil {
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

func (s *MessageIngestionService) handleDebugCommand(ctx context.Context, message vk.IncomingMessage) error {
	if !s.isManualSenderAllowed(message.SenderID) {
		s.logger.Info("debug command rejected: sender is not allowed",
			slog.Int64("sender_id", message.SenderID),
			slog.Int64("peer_id", message.PeerID),
		)
		return nil
	}

	ping := time.Duration(0)
	pingErr := error(nil)
	if s.pinger != nil {
		pingCtx, cancel := context.WithTimeout(ctx, 5*time.Second)
		ping, pingErr = s.pinger.Ping(pingCtx)
		cancel()
	}
	if pingErr != nil {
		s.logger.Warn("debug vk ping failed", slog.String("error", pingErr.Error()))
	}

	today, err := s.repo.LLMUsageToday(ctx, "Europe/Moscow")
	if err != nil {
		return fmt.Errorf("load today llm usage: %w", err)
	}
	daily, err := s.repo.DailyLLMUsage(ctx, 7, "Europe/Moscow")
	if err != nil {
		return fmt.Errorf("load daily llm usage: %w", err)
	}
	month, err := s.repo.LLMUsageDays(ctx, 30, "Europe/Moscow")
	if err != nil {
		return fmt.Errorf("load 30 days llm usage: %w", err)
	}

	messageText := formatLLMUsageDebug(s.llmModel, ping, pingErr, today, daily, month)
	formatData := buildDebugFormatData(messageText)
	chart, chartErr := renderLLMUsageChart(daily)
	if chartErr != nil {
		s.logger.Warn("failed to render llm usage chart", slog.String("error", chartErr.Error()))
		if err := s.publisher.PublishFormatted(ctx, message.PeerID, messageText, formatData); err != nil {
			return fmt.Errorf("publish debug usage: %w", err)
		}
		return nil
	}
	if err := s.publisher.PublishFormattedWithImage(ctx, message.PeerID, messageText, formatData, chart); err != nil {
		return fmt.Errorf("publish debug usage with chart: %w", err)
	}
	return nil
}

type debugFormatData struct {
	Version int               `json:"version"`
	Items   []debugFormatItem `json:"items"`
}

type debugFormatItem struct {
	Type   string `json:"type"`
	Offset int    `json:"offset"`
	Length int    `json:"length"`
}

func buildDebugFormatData(text string) string {
	const title = "LLM usage"
	idx := strings.Index(text, title)
	if idx < 0 {
		return ""
	}
	data, err := json.Marshal(debugFormatData{
		Version: 1,
		Items: []debugFormatItem{{
			Type:   "bold",
			Offset: utf16Units(text[:idx]),
			Length: utf16Units(title),
		}},
	})
	if err != nil {
		return ""
	}
	return string(data)
}

func utf16Units(text string) int {
	return len(utf16.Encode([]rune(text)))
}

func formatLLMUsageDebug(model string, ping time.Duration, pingErr error, today storage.LLMUsageTotals, daily []storage.DailyLLMUsage, month storage.LLMUsageTotals) string {
	var b strings.Builder
	b.WriteString("📊 LLM usage\n")
	b.WriteString("model=")
	b.WriteString(formatModelName(model))
	b.WriteString("\n")
	b.WriteString("ping=")
	b.WriteString(formatPing(ping, pingErr))
	b.WriteString("\n\n")

	todayLabel := "сегодня"
	if len(daily) > 0 && strings.TrimSpace(daily[0].Day) != "" {
		todayLabel = daily[0].Day
	}

	b.WriteString("🕛 Сегодня с 00:00 МСК:\n")
	b.WriteString(formatUsageLine(model, todayLabel, today.SummaryCount, today.ChatCount, today.PromptTokens, today.CachedPromptTokens, today.CompletionTokens, today.AvgLatencyMs))
	b.WriteString("\n\n📅 Последние 7 дней:\n")
	if len(daily) == 0 {
		b.WriteString("\nнет данных")
		return b.String()
	}
	for i, day := range daily {
		if i > 0 {
			b.WriteByte('\n')
		}
		b.WriteString(formatUsageLine(model, day.Day, day.SummaryCount, day.ChatCount, day.PromptTokens, day.CachedPromptTokens, day.CompletionTokens, day.AvgLatencyMs))
	}
	b.WriteString("\n\n📅 Последние 30 дней:\n")
	b.WriteString(formatUsageLine(model, "итого", month.SummaryCount, month.ChatCount, month.PromptTokens, month.CachedPromptTokens, month.CompletionTokens, month.AvgLatencyMs))
	return b.String()
}

func formatPing(ping time.Duration, err error) string {
	if err != nil {
		return "error"
	}
	if ping <= 0 {
		return "-"
	}
	return formatDurationMs(ping.Milliseconds())
}

func formatUsageLine(model string, label string, summaries, chats int, inputTokens, cachedInputTokens, outputTokens, avgLatencyMs int64) string {
	return fmt.Sprintf("%s: posts=%d, chats=%d, input=%s, cached_input=%s, output=%s, cost=%s, avg=%s", formatUsageLabel(label), summaries, chats, formatTokenCount(inputTokens), formatTokenCount(cachedInputTokens), formatTokenCount(outputTokens), formatLLMCost(model, inputTokens, cachedInputTokens, outputTokens), formatDurationMs(avgLatencyMs))
}

func formatUsageLabel(label string) string {
	label = strings.TrimSpace(label)
	if len(label) == len("2006-01-02") && label[4] == '-' && label[7] == '-' {
		return label[8:10] + "." + label[5:7]
	}
	if label == "" {
		return "-"
	}
	return label
}

type llmTokenPrice struct {
	InputPerMillion       float64
	CachedInputPerMillion float64
	OutputPerMillion      float64
}

func formatLLMCost(model string, inputTokens, cachedInputTokens, outputTokens int64) string {
	price, ok := llmPrice(model)
	if !ok {
		return "-"
	}
	regularInputTokens := inputTokens - cachedInputTokens
	if regularInputTokens < 0 {
		regularInputTokens = 0
	}
	cost := float64(regularInputTokens)*price.InputPerMillion/1_000_000 + float64(cachedInputTokens)*price.CachedInputPerMillion/1_000_000 + float64(outputTokens)*price.OutputPerMillion/1_000_000
	return formatUSD(cost)
}

func llmPrice(model string) (llmTokenPrice, bool) {
	model = strings.TrimSpace(model)
	switch {
	case model == "gpt-5-chat-latest" || model == "chat-latest":
		return llmTokenPrice{InputPerMillion: 5.00, CachedInputPerMillion: 0.50, OutputPerMillion: 30.00}, true
	case strings.HasPrefix(model, "gpt-5.4-mini"):
		return llmTokenPrice{InputPerMillion: 0.75, CachedInputPerMillion: 0.075, OutputPerMillion: 4.50}, true
	case strings.HasPrefix(model, "gpt-5.4-nano"):
		return llmTokenPrice{InputPerMillion: 0.20, CachedInputPerMillion: 0.020, OutputPerMillion: 1.25}, true
	case strings.HasPrefix(model, "gpt-5.4"):
		return llmTokenPrice{InputPerMillion: 2.50, CachedInputPerMillion: 0.25, OutputPerMillion: 15.00}, true
	case strings.HasPrefix(model, "gpt-5.5"):
		return llmTokenPrice{InputPerMillion: 5.00, CachedInputPerMillion: 0.50, OutputPerMillion: 30.00}, true
	}
	return llmTokenPrice{}, false
}

func formatUSD(cost float64) string {
	if cost <= 0 {
		return "$0.00"
	}
	return fmt.Sprintf("$%.2f", math.Ceil(cost*100)/100)
}

func formatModelName(model string) string {
	model = strings.TrimSpace(model)
	if model == "" {
		return "-"
	}
	return model
}

func formatDurationMs(ms int64) string {
	if ms <= 0 {
		return "0s"
	}
	if ms < 1000 {
		return fmt.Sprintf("%dms", ms)
	}
	return fmt.Sprintf("%.1fs", float64(ms)/1000)
}

func formatTokenCount(value int64) string {
	if value < 1000 {
		return fmt.Sprintf("%d", value)
	}
	if value < 1_000_000 {
		thousands := (value + 500) / 1000
		if thousands >= 1000 {
			return "1kk"
		}
		return fmt.Sprintf("%dk", thousands)
	}
	millions := (value + 500_000) / 1_000_000
	return fmt.Sprintf("%dkk", millions)
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
		remaining := result.RequiredCount - result.MeaningfulCount
		if remaining < 0 {
			remaining = 0
		}
		return s.publisher.Publish(ctx, peerID, fmt.Sprintf("Уперлись в почасовой лимит LLM. Контекст сохранен, следующая автопопытка будет после того, как в этой конфе накопится еще %d осмысленных сообщений.", remaining))
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

	go s.runAutoSummary(chatID, peerID)
}

func (s *MessageIngestionService) runAutoSummary(chatID, peerID int64) {
	for {
		ctx, cancel := context.WithTimeout(context.Background(), s.manualExecutionTimeout)
		err := s.handleAutoSummary(ctx, chatID, peerID)
		cancel()
		if err != nil {
			s.logger.Error("automatic summary failed",
				slog.Int64("chat_id", chatID),
				slog.Int64("peer_id", peerID),
				slog.String("error", err.Error()),
			)
		}

		s.autoMu.Lock()
		state := s.autoRuns[peerID]
		if state == nil || !state.pending {
			delete(s.autoRuns, peerID)
			s.autoMu.Unlock()
			return
		}
		state.pending = false
		s.autoMu.Unlock()
	}
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
		case summary.RunStatusLocked, summary.RunStatusAlreadyProcessed, summary.RunStatusNotEnoughMessages, summary.RunStatusNoMessages, summary.RunStatusRateLimited:
			return nil
		default:
			s.logger.Warn("automatic summary returned unexpected status", slog.String("status", string(result.Status)))
			return nil
		}
	}
}

func manualSummaryFailureMessage(err error) string {
	if errors.Is(err, context.DeadlineExceeded) || errors.Is(err, context.Canceled) {
		return "Не смог собрать summary: операция заняла слишком много времени. Контекст сохранен, можно повторить позже."
	}
	var statusErr *llm.HTTPStatusError
	if errors.As(err, &statusErr) {
		return fmt.Sprintf("Не смог собрать summary: LLM вернула HTTP %d: %s. Контекст сохранен, можно повторить позже.", statusErr.StatusCode, statusErr.PublicMessage())
	}
	message := strings.ToLower(err.Error())
	switch {
	case strings.Contains(message, "generate summary"):
		return "Не смог собрать summary: LLM сейчас не ответила или вернула ошибку. Контекст сохранен, можно повторить позже."
	case strings.Contains(message, "publish summary"):
		return "Summary собрал, но не смог отправить его в VK. Контекст сохранен, можно повторить позже."
	case strings.Contains(message, "collect candidate"), strings.Contains(message, "load previous summary"), strings.Contains(message, "persist"), strings.Contains(message, "reset summary chat state"):
		return "Не смог собрать summary из-за ошибки хранилища. Контекст сохранен, подробности в логах."
	default:
		return "Не смог собрать summary из-за внутренней ошибки. Контекст сохранен, подробности в логах."
	}
}

func isChatMessage(peerID int64) bool {
	const peerOffset int64 = 2_000_000_000
	return peerID > peerOffset
}
