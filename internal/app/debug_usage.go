package app

import (
	"context"
	"encoding/json"
	"fmt"
	"log/slog"
	"math/rand"
	"strings"
	"time"
	"unicode/utf16"

	"bot-summary-vk/internal/storage"
	"bot-summary-vk/internal/vk"
)

const (
	debugDetailsButtonLabel    = "📊 Расширенная статистика"
	debugDetailsPayloadCommand = "livanda_debug_details"
	debugSummaryButtonLabel    = "🧹 Краткая статистика"
	debugSummaryPayloadCommand = "livanda_debug_summary"
)

var debugUnauthorizedPhrases = []string{
	"Редактор ударил по рукам: не трожь!",
	"Корректор шипит из-под стола: доступ закрыт.",
	"Архивариус захлопнул папку прямо перед носом.",
	"Редакция делает вид, что вас тут не было.",
	"Печатная машинка сказала: только для своих.",
	"Секретарь спрятал кнопку в сейф.",
	"Главред поднял бровь: рано вам в статистику.",
	"Типография не пускает без редакционного пропуска.",
	"Красный карандаш лег поперек дороги.",
	"Редактор кашлянул и убрал руку с кнопки.",
	"Вахтер редакции попросил не шуметь у графиков.",
	"Статистика спряталась за шкаф и не выходит.",
	"Стажер унес ключ от этой кнопки.",
	"Редакционная дверь скрипнула и закрылась.",
	"Бухгалтерия сказала: сначала станьте админом.",
	"Кнопка посмотрела строго и отказалась.",
	"Редакторский котелок закипел: доступ запрещен.",
	"Папка с цифрами ушла на обед.",
	"Верстальщик заслонил экран газетой.",
	"Главред поставил печать: нельзя.",
	"Архив выдал вам квитанцию без права доступа.",
	"Счетчик токенов спрятался от посторонних глаз.",
	"Редакционный замок щелкнул раньше вас.",
	"Секретный график сделал вид, что он обои.",
	"Цифры попросили предъявить админский мандат.",
	"Корректор сказал: руки прочь от служебной полосы.",
	"Редактор снял очки и молча покачал головой.",
	"Кнопка не ваша, гражданин читатель.",
	"Внутренняя кухня закрыта на санитарный день.",
	"Статистику унесли в комнату для взрослых редакторов.",
	"Панель управления спряталась в редакционном дыму.",
	"Главред шлепнул линейкой по пальцам.",
	"Дежурный по типографии сказал: прохода нет.",
	"Админский жетон не найден, кнопка молчит.",
	"Редакция любит любопытных, но не настолько.",
	"Комната с расходами закрыта на крючок.",
	"Кнопка делает вид, что вы нажали стену.",
	"Служебная статистика ушла в подполье.",
	"Редакторский турникет вас не признал.",
	"Цифры надели темные очки и отвернулись.",
	"Бухгалтерский сейф фыркнул и не открылся.",
	"Печатный станок требует админский пароль.",
	"Редакционный сторож попросил выйти из графиков.",
	"Кнопка откусила палец и закрылась.",
	"Сводка не любит случайных прохожих.",
	"Таблица расходов спряталась под скатерть.",
	"Редактор сказал: сначала бейджик, потом кнопки.",
	"Служебный люк захлопнулся с драматическим стуком.",
	"График показал язык и убежал.",
	"Редакция постановила: любопытство без допуска не кормить.",
}

type debugKeyboardPublisher interface {
	PublishFormattedWithKeyboard(ctx context.Context, peerID int64, text string, formatData string, keyboard string) error
}

type debugImageKeyboardPublisher interface {
	PublishFormattedWithImageKeyboard(ctx context.Context, peerID int64, text string, formatData string, image []byte, keyboard string) error
}

type debugMessageEditor interface {
	EditFormattedMessage(ctx context.Context, peerID, conversationMessageID int64, text string, formatData string, keyboard string) error
	EditFormattedMessageWithImage(ctx context.Context, peerID, conversationMessageID int64, text string, formatData string, image []byte, keyboard string) error
}

type debugEventResponder interface {
	AnswerMessageEvent(ctx context.Context, eventID string, userID, peerID int64, text string) error
}

func (s *MessageIngestionService) handleDebugCommand(ctx context.Context, message vk.IncomingMessage, show7Days bool) error {
	if !s.isManualSenderAllowed(message.SenderID) {
		s.logger.Info("debug command rejected: sender is not allowed",
			slog.Int64("sender_id", message.SenderID),
			slog.Int64("peer_id", message.PeerID),
		)
		return nil
	}
	return s.publishDebugUsage(ctx, message.PeerID, 0, show7Days)
}

func (s *MessageIngestionService) publishDebugUsage(ctx context.Context, peerID int64, conversationMessageID int64, show7Days bool) error {
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

	daily, err := s.repo.DailyLLMUsage(ctx, 7, "Europe/Moscow")
	if err != nil {
		return fmt.Errorf("load daily llm usage: %w", err)
	}
	month, err := s.repo.LLMUsageDays(ctx, 30, "Europe/Moscow")
	if err != nil {
		return fmt.Errorf("load 30 days llm usage: %w", err)
	}
	imageDaily, err := s.repo.DailyImageUsage(ctx, 7, "Europe/Moscow")
	if err != nil {
		return fmt.Errorf("load daily image usage: %w", err)
	}
	imageMonth, err := s.repo.ImageUsageDays(ctx, 30, "Europe/Moscow")
	if err != nil {
		return fmt.Errorf("load 30 days image usage: %w", err)
	}

	messageText := formatLLMUsageDebug(s.llmModel, s.imagePromptLLMModel, s.imageModel, ping, pingErr, daily, month, imageDaily, imageMonth, show7Days)
	formatData := buildDebugFormatData(messageText)
	keyboard := buildDebugKeyboard(show7Days)
	chart, chartErr := renderUsageChart(daily, imageDaily)
	if chartErr != nil {
		s.logger.Warn("failed to render llm usage chart", slog.String("error", chartErr.Error()))
		if conversationMessageID > 0 {
			return s.editDebugUsageText(ctx, peerID, conversationMessageID, messageText, formatData, keyboard)
		}
		if publisher, ok := s.publisher.(debugKeyboardPublisher); ok && keyboard != "" {
			if err := publisher.PublishFormattedWithKeyboard(ctx, peerID, messageText, formatData, keyboard); err != nil {
				return fmt.Errorf("publish debug usage with keyboard: %w", err)
			}
			return nil
		}
		if err := s.publisher.PublishFormatted(ctx, peerID, messageText, formatData); err != nil {
			return fmt.Errorf("publish debug usage: %w", err)
		}
		return nil
	}

	if conversationMessageID > 0 {
		return s.editDebugUsageImage(ctx, peerID, conversationMessageID, messageText, formatData, chart, keyboard)
	}
	if publisher, ok := s.publisher.(debugImageKeyboardPublisher); ok && keyboard != "" {
		if err := publisher.PublishFormattedWithImageKeyboard(ctx, peerID, messageText, formatData, chart, keyboard); err != nil {
			return fmt.Errorf("publish debug usage with chart and keyboard: %w", err)
		}
		return nil
	}
	if err := s.publisher.PublishFormattedWithImage(ctx, peerID, messageText, formatData, chart); err != nil {
		return fmt.Errorf("publish debug usage with chart: %w", err)
	}
	return nil
}

func (s *MessageIngestionService) editDebugUsageText(ctx context.Context, peerID int64, conversationMessageID int64, text string, formatData string, keyboard string) error {
	editor, ok := s.publisher.(debugMessageEditor)
	if !ok {
		return fmt.Errorf("debug message editor is not available")
	}
	if err := editor.EditFormattedMessage(ctx, peerID, conversationMessageID, text, formatData, keyboard); err != nil {
		return fmt.Errorf("edit debug usage: %w", err)
	}
	return nil
}

func (s *MessageIngestionService) editDebugUsageImage(ctx context.Context, peerID int64, conversationMessageID int64, text string, formatData string, image []byte, keyboard string) error {
	editor, ok := s.publisher.(debugMessageEditor)
	if !ok {
		return fmt.Errorf("debug message editor is not available")
	}
	if err := editor.EditFormattedMessageWithImage(ctx, peerID, conversationMessageID, text, formatData, image, keyboard); err != nil {
		return fmt.Errorf("edit debug usage with chart: %w", err)
	}
	return nil
}

type debugKeyboard struct {
	Inline  bool                    `json:"inline"`
	Buttons [][]debugKeyboardButton `json:"buttons"`
}

type debugKeyboardButton struct {
	Action debugKeyboardAction `json:"action"`
	Color  string              `json:"color"`
}

type debugKeyboardAction struct {
	Type    string `json:"type"`
	Label   string `json:"label"`
	Payload string `json:"payload"`
}

func buildDebugKeyboard(show7Days bool) string {
	label := debugDetailsButtonLabel
	command := debugDetailsPayloadCommand
	if show7Days {
		label = debugSummaryButtonLabel
		command = debugSummaryPayloadCommand
	}

	payload, err := json.Marshal(debugEventPayload{Command: command})
	if err != nil {
		return ""
	}
	keyboard, err := json.Marshal(debugKeyboard{
		Inline: true,
		Buttons: [][]debugKeyboardButton{{
			{
				Action: debugKeyboardAction{
					Type:    "callback",
					Label:   label,
					Payload: string(payload),
				},
				Color: "secondary",
			},
		}},
	})
	if err != nil {
		return ""
	}
	return string(keyboard)
}

type debugEventPayload struct {
	Command string `json:"command"`
}

func debugPayloadCommand(raw json.RawMessage) string {
	payload := debugEventPayload{}
	if err := json.Unmarshal(raw, &payload); err == nil {
		return knownDebugPayloadCommand(payload.Command)
	}

	var encoded string
	if err := json.Unmarshal(raw, &encoded); err != nil {
		return ""
	}
	if err := json.Unmarshal([]byte(encoded), &payload); err != nil {
		return ""
	}
	return knownDebugPayloadCommand(payload.Command)
}

func knownDebugPayloadCommand(command string) string {
	switch command {
	case debugDetailsPayloadCommand, debugSummaryPayloadCommand:
		return command
	default:
		return ""
	}
}

func randomDebugUnauthorizedPhrase() string {
	if len(debugUnauthorizedPhrases) == 0 {
		return "Редактор ударил по рукам: не трожь!"
	}
	return debugUnauthorizedPhrases[rand.Intn(len(debugUnauthorizedPhrases))]
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
	titles := []string{"LLM usage", "Image usage", "Monthly costs"}
	items := make([]debugFormatItem, 0, len(titles))
	for _, title := range titles {
		idx := strings.Index(text, title)
		if idx < 0 {
			continue
		}
		items = append(items, debugFormatItem{
			Type:   "bold",
			Offset: utf16Units(text[:idx]),
			Length: utf16Units(title),
		})
	}
	items = append(items, buildDebugCostFormatItems(text)...)
	if len(items) == 0 {
		return ""
	}
	data, err := json.Marshal(debugFormatData{
		Version: 1,
		Items:   items,
	})
	if err != nil {
		return ""
	}
	return string(data)
}

func buildDebugCostFormatItems(text string) []debugFormatItem {
	items := []debugFormatItem{}
	searchFrom := 0
	for {
		costIdx := strings.Index(text[searchFrom:], "cost=")
		if costIdx < 0 {
			break
		}
		costIdx += searchFrom
		end := costIdx
		for end < len(text) && text[end] != '\n' && text[end] != ',' && text[end] != ' ' {
			end++
		}
		items = append(items, debugFormatItem{
			Type:   "bold",
			Offset: utf16Units(text[:costIdx]),
			Length: utf16Units(text[costIdx:end]),
		})
		searchFrom = end
	}
	return items
}

func utf16Units(text string) int {
	return len(utf16.Encode([]rune(text)))
}

func formatLLMUsageDebug(
	model string,
	imagePromptModel string,
	imageModel string,
	ping time.Duration,
	pingErr error,
	daily []storage.DailyLLMUsage,
	month storage.LLMUsageTotals,
	imageDaily []storage.DailyImageUsage,
	imageMonth storage.ImageUsageTotals,
	show7Days bool,
) string {
	var b strings.Builder
	b.WriteString("📊 LLM usage  ")
	b.WriteString("model=")
	b.WriteString(formatModelName(model))
	b.WriteString("  ")
	b.WriteString("ping=")
	b.WriteString(formatPing(ping, pingErr))
	b.WriteString("\n\n")

	b.WriteString("🖼 Image usage  ")
	b.WriteString("prompt_model=")
	b.WriteString(formatModelName(imagePromptModel))
	b.WriteString("  ")
	b.WriteString("image_model=")
	b.WriteString(formatModelName(imageModel))
	b.WriteString("  ")
	b.WriteString("ping=")
	b.WriteString(formatPing(ping, pingErr))
	b.WriteString("\n\n")

	if !show7Days {
		b.WriteString("🥇 Monthly costs:\n")
		b.WriteString("text: ")
		b.WriteString(formatLLMCost(model, month.PromptTokens, month.CachedPromptTokens, month.CompletionTokens))
		b.WriteByte('\n')
		b.WriteString("images: ")
		b.WriteString(formatImageCost(imagePromptModel, imageModel, imageMonth.PromptLLMPromptTokens, imageMonth.PromptLLMCachedPromptTokens, imageMonth.PromptLLMCompletionTokens, imageMonth.ImageInputTextTokens, imageMonth.ImageInputImageTokens, imageMonth.ImageOutputTokens))
		return b.String()
	}

	b.WriteString("📅 Последние 7 дней:\n")
	if len(daily) == 0 {
		b.WriteString("нет данных\n")
	} else {
		imageDailyByDay := mapDailyImageUsage(imageDaily)
		for i, day := range daily {
			if i > 0 {
				b.WriteString("\n\n")
			}
			b.WriteString(formatUsageLabel(day.Day))
			b.WriteString(":\n")
			b.WriteString(formatUsageTotalLine(model, day.SummaryCount, day.ChatCount, day.PromptTokens, day.CachedPromptTokens, day.CompletionTokens, day.AvgLatencyMs))
			imageDay := imageDailyByDay[day.Day]
			b.WriteByte('\n')
			b.WriteString(formatImageUsageTotalLine(imagePromptModel, imageModel, imageDay.ImageCount, imageDay.PromptLLMPromptTokens, imageDay.PromptLLMCachedPromptTokens, imageDay.PromptLLMCompletionTokens, imageDay.ImageInputTokens, imageDay.ImageInputTextTokens, imageDay.ImageInputImageTokens, imageDay.ImageOutputTokens, imageDay.AvgPromptLLMLatencyMs, imageDay.AvgImageLatencyMs))
		}
	}
	b.WriteString("\n\n")
	b.WriteString("📅 Последние 30 дней:\n")
	b.WriteString(formatUsageTotalLine(model, month.SummaryCount, month.ChatCount, month.PromptTokens, month.CachedPromptTokens, month.CompletionTokens, month.AvgLatencyMs))
	b.WriteByte('\n')
	b.WriteString(formatImageUsageTotalLine(imagePromptModel, imageModel, imageMonth.ImageCount, imageMonth.PromptLLMPromptTokens, imageMonth.PromptLLMCachedPromptTokens, imageMonth.PromptLLMCompletionTokens, imageMonth.ImageInputTokens, imageMonth.ImageInputTextTokens, imageMonth.ImageInputImageTokens, imageMonth.ImageOutputTokens, imageMonth.AvgPromptLLMLatencyMs, imageMonth.AvgImageLatencyMs))
	return b.String()
}

func mapDailyImageUsage(stats []storage.DailyImageUsage) map[string]storage.DailyImageUsage {
	byDay := make(map[string]storage.DailyImageUsage, len(stats))
	for _, stat := range stats {
		byDay[stat.Day] = stat
	}
	return byDay
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

func formatUsageTotalLine(model string, summaries, chats int, inputTokens, cachedInputTokens, outputTokens, avgLatencyMs int64) string {
	return fmt.Sprintf("text: posts=%d, chats=%d, input=%s, output=%s, avg=%s, cost=%s", summaries, chats, formatTokenCount(inputTokens), formatTokenCount(outputTokens), formatDurationMs(avgLatencyMs), formatLLMCost(model, inputTokens, cachedInputTokens, outputTokens))
}

func formatImageUsageTotalLine(promptModel, imageModel string, images int, promptInputTokens, promptCachedInputTokens, promptOutputTokens, imageInputTokens, imageTextInputTokens, imageImageInputTokens, imageOutputTokens, promptAvgLatencyMs, imageAvgLatencyMs int64) string {
	return fmt.Sprintf(
		"images: images=%d, prompt_input=%s, prompt_output=%s, image_input=%s, image_output=%s, prompt_avg=%s, image_avg=%s, cost=%s",
		images,
		formatTokenCount(promptInputTokens),
		formatTokenCount(promptOutputTokens),
		formatTokenCount(imageInputTokens),
		formatTokenCount(imageOutputTokens),
		formatDurationMs(promptAvgLatencyMs),
		formatDurationMs(imageAvgLatencyMs),
		formatImageCost(promptModel, imageModel, promptInputTokens, promptCachedInputTokens, promptOutputTokens, imageTextInputTokens, imageImageInputTokens, imageOutputTokens),
	)
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
