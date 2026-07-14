package app

import (
	"context"
	"encoding/json"
	"fmt"
	"log/slog"
	"strings"
	"time"
	"unicode/utf16"

	"bot-summary-vk/internal/storage"
	"bot-summary-vk/internal/vk"
)

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

	messageText := formatLLMUsageDebug(s.llmModel, s.imagePromptLLMModel, s.imageModel, ping, pingErr, daily, month, imageDaily, imageMonth)
	formatData := buildDebugFormatData(messageText)
	chart, chartErr := renderUsageChart(daily, imageDaily)
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
	titles := []string{"LLM usage", "Image usage"}
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
) string {
	var b strings.Builder
	b.WriteString("📊 LLM usage  ")
	b.WriteString("model=")
	b.WriteString(formatModelName(model))
	b.WriteString("  ")
	b.WriteString("ping=")
	b.WriteString(formatPing(ping, pingErr))
	b.WriteString("\n\n")

	b.WriteString("📅 Последние 7 дней:\n")
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

	b.WriteString("\n\n🖼 Image usage  ")
	b.WriteString("prompt_model=")
	b.WriteString(formatModelName(imagePromptModel))
	b.WriteString("  ")
	b.WriteString("image_model=")
	b.WriteString(formatModelName(imageModel))
	b.WriteString("  ")
	b.WriteString("ping=")
	b.WriteString(formatPing(ping, pingErr))
	b.WriteString("\n\n")
	b.WriteString("📅 Последние 7 дней:\n")
	if len(imageDaily) == 0 {
		b.WriteString("\nнет данных")
		return b.String()
	}
	for i, day := range imageDaily {
		if i > 0 {
			b.WriteByte('\n')
		}
		b.WriteString(formatImageUsageLine(imagePromptModel, imageModel, day.Day, day.ImageCount, day.ChatCount, day.PromptLLMPromptTokens, day.PromptLLMCachedPromptTokens, day.PromptLLMCompletionTokens, day.ImageInputTokens, day.ImageInputTextTokens, day.ImageInputImageTokens, day.ImageOutputTokens, day.AvgPromptLLMLatencyMs, day.AvgImageLatencyMs))
	}
	b.WriteString("\n\n📅 Последние 30 дней:\n")
	b.WriteString(formatImageUsageLine(imagePromptModel, imageModel, "итого", imageMonth.ImageCount, imageMonth.ChatCount, imageMonth.PromptLLMPromptTokens, imageMonth.PromptLLMCachedPromptTokens, imageMonth.PromptLLMCompletionTokens, imageMonth.ImageInputTokens, imageMonth.ImageInputTextTokens, imageMonth.ImageInputImageTokens, imageMonth.ImageOutputTokens, imageMonth.AvgPromptLLMLatencyMs, imageMonth.AvgImageLatencyMs))
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
	return fmt.Sprintf("%s: posts=%d, chats=%d, input=%s, output=%s, cost=%s, avg=%s", formatUsageLabel(label), summaries, chats, formatTokenCount(inputTokens), formatTokenCount(outputTokens), formatLLMCost(model, inputTokens, cachedInputTokens, outputTokens), formatDurationMs(avgLatencyMs))
}

func formatImageUsageLine(promptModel, imageModel, label string, images, chats int, promptInputTokens, promptCachedInputTokens, promptOutputTokens, imageInputTokens, imageTextInputTokens, imageImageInputTokens, imageOutputTokens, promptAvgLatencyMs, imageAvgLatencyMs int64) string {
	return fmt.Sprintf(
		"%s: images=%d, chats=%d, prompt_input=%s, prompt_output=%s, image_input=%s, image_output=%s, cost=%s, prompt_avg=%s, image_avg=%s",
		formatUsageLabel(label),
		images,
		chats,
		formatTokenCount(promptInputTokens),
		formatTokenCount(promptOutputTokens),
		formatTokenCount(imageInputTokens),
		formatTokenCount(imageOutputTokens),
		formatImageCost(promptModel, imageModel, promptInputTokens, promptCachedInputTokens, promptOutputTokens, imageTextInputTokens, imageImageInputTokens, imageOutputTokens),
		formatDurationMs(promptAvgLatencyMs),
		formatDurationMs(imageAvgLatencyMs),
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
