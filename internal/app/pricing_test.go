package app

import (
	"math"
	"strings"
	"testing"

	"bot-summary-vk/internal/storage"
)

func TestRecordedLLMCostAcrossModelChange(t *testing.T) {
	requests := []storage.LLMRequestUsage{
		{Model: "gpt-5.6-luna", PromptTokens: 100_000, CachedPromptTokens: 20_000, CompletionTokens: 10_000},
		{Model: "gpt-5-nano", PromptTokens: 100_000, CachedPromptTokens: 20_000, CompletionTokens: 10_000},
	}
	// Luna $0.0284 + nano $0.0081 = $0.0365, rounded up once.
	if got := formatRecordedLLMCost(requests, 200_000, 40_000, 20_000); got != "$0.04" {
		t.Fatalf("mixed-model cost = %s, want $0.04", got)
	}
	text := formatLLMUsageDebug("gpt-5-nano", "gpt-5.4-nano", "gpt-image-1-mini", 0, nil, nil,
		storage.LLMUsageTotals{Requests: requests, PromptTokens: 200_000, CachedPromptTokens: 40_000, CompletionTokens: 20_000}, nil, storage.ImageUsageTotals{}, false)
	if !strings.Contains(text, "Monthly costs:\ntext: $0.04") {
		t.Fatalf("unexpected monthly report: %s", text)
	}
	if got := formatRecordedLLMCost(requests[:1], 200_000, 40_000, 20_000); got != "-" {
		t.Fatalf("missing history cost = %s", got)
	}
	requests[1].Model = "unknown"
	if got := formatRecordedLLMCost(requests, 200_000, 40_000, 20_000); got != "-" {
		t.Fatalf("unknown model cost = %s", got)
	}
}

func TestLLMCostRates(t *testing.T) {
	for _, tc := range []struct {
		model                 string
		input, cached, output int64
		want                  float64
	}{
		{"gpt-5-nano", 1_000_000, 200_000, 1_000_000, 0.441},
		{"gpt-5-nano-2025-08-07", 1_000_000, 200_000, 1_000_000, 0.441},
		{"gpt-5.6-luna", 272_000, 20_000, 10_000, 0.0628},
		{"gpt-5.6-luna", 300_000, 20_000, 10_000, 0.1308},
	} {
		got, ok := llmCostValue(tc.model, tc.input, tc.cached, tc.output)
		if !ok || math.Abs(got-tc.want) > 1e-10 {
			t.Errorf("%s (%d input): got %v, %v; want %v", tc.model, tc.input, got, ok, tc.want)
		}
	}
	if got := formatLLMCost("gpt-5-nano", 0, 0, 0); got != "$0.00" {
		t.Fatalf("empty usage cost = %s", got)
	}
}

func TestLLMChatModelPrices(t *testing.T) {
	tests := []struct {
		model string
		want  llmTokenPrice
	}{
		{
			model: "gpt-5.3-chat-latest",
			want:  llmTokenPrice{InputPerMillion: 1.75, CachedInputPerMillion: 0.175, OutputPerMillion: 14.00},
		},
		{
			model: "gpt-5-chat-latest",
			want:  llmTokenPrice{InputPerMillion: 1.25, CachedInputPerMillion: 0.125, OutputPerMillion: 10.00},
		},
		{
			model: "chat-latest",
			want:  llmTokenPrice{InputPerMillion: 5.00, CachedInputPerMillion: 0.50, OutputPerMillion: 30.00},
		},
	}

	for _, tt := range tests {
		t.Run(tt.model, func(t *testing.T) {
			got, ok := llmPrice(tt.model)
			if !ok {
				t.Fatalf("llmPrice() did not recognize %s", tt.model)
			}
			if got != tt.want {
				t.Fatalf("llmPrice() = %+v, want %+v", got, tt.want)
			}
		})
	}
}

func TestImagePromptCostAcrossModelChange(t *testing.T) {
	requests := []storage.LLMRequestUsage{
		{Model: "gpt-5.4-nano", PromptTokens: 100_000, CachedPromptTokens: 20_000, CompletionTokens: 10_000},
		{Model: "gpt-5-nano", PromptTokens: 100_000, CachedPromptTokens: 20_000, CompletionTokens: 10_000},
	}
	// Old prompt $0.0289 + new prompt $0.0081 + image $0.008.
	if got := formatImageCost(requests, "gpt-image-1-mini", 200_000, 40_000, 20_000, 0, 0, 1000); got != "$0.05" {
		t.Fatalf("image cost = %s, want $0.05", got)
	}
	if got := formatImageCost(nil, "gpt-image-1-mini", 200_000, 40_000, 20_000, 0, 0, 1000); got != "-" {
		t.Fatalf("missing prompt history cost = %s", got)
	}
}
