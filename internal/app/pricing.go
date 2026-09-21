package app

import (
	"fmt"
	"math"
	"strings"

	"bot-summary-vk/internal/storage"
)

type llmTokenPrice struct {
	InputPerMillion       float64
	CachedInputPerMillion float64
	OutputPerMillion      float64
}

func formatLLMCost(model string, inputTokens, cachedInputTokens, outputTokens int64) string {
	cost, ok := llmCostValue(model, inputTokens, cachedInputTokens, outputTokens)
	if !ok {
		return "-"
	}
	return formatUSD(cost)
}

func llmCostValue(model string, inputTokens, cachedInputTokens, outputTokens int64) (float64, bool) {
	price, ok := llmPrice(model)
	if !ok {
		return 0, false
	}
	// Luna charges the full request at long-context rates above 272K input tokens.
	if strings.TrimSpace(model) == "gpt-5.6-luna" && inputTokens > 272_000 {
		price.InputPerMillion *= 2
		price.CachedInputPerMillion *= 2
		price.OutputPerMillion *= 1.5
	}
	regularInputTokens := inputTokens - cachedInputTokens
	if regularInputTokens < 0 {
		regularInputTokens = 0
	}
	cost := float64(regularInputTokens)*price.InputPerMillion/1_000_000 + float64(cachedInputTokens)*price.CachedInputPerMillion/1_000_000 + float64(outputTokens)*price.OutputPerMillion/1_000_000
	return cost, true
}

func llmPrice(model string) (llmTokenPrice, bool) {
	model = strings.TrimSpace(model)
	// Standard API prices: https://developers.openai.com/api/docs/pricing
	switch {
	case model == "gpt-5-nano" || model == "gpt-5-nano-2025-08-07":
		return llmTokenPrice{InputPerMillion: 0.05, CachedInputPerMillion: 0.005, OutputPerMillion: 0.40}, true
	case model == "gpt-5.6-luna":
		return llmTokenPrice{InputPerMillion: 0.20, CachedInputPerMillion: 0.02, OutputPerMillion: 1.20}, true
	case model == "gpt-5.3-chat-latest":
		return llmTokenPrice{InputPerMillion: 1.75, CachedInputPerMillion: 0.175, OutputPerMillion: 14.00}, true
	case model == "gpt-5-chat-latest":
		return llmTokenPrice{InputPerMillion: 1.25, CachedInputPerMillion: 0.125, OutputPerMillion: 10.00}, true
	case model == "chat-latest":
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

type imageTokenPrice struct {
	TextInputPerMillion  float64
	ImageInputPerMillion float64
	OutputPerMillion     float64
}

func formatImageCost(promptModel, imageModel string, promptInputTokens, promptCachedInputTokens, promptOutputTokens, imageTextInputTokens, imageImageInputTokens, imageOutputTokens int64) string {
	cost := 0.0
	known := false
	if promptCost, ok := llmCostValue(promptModel, promptInputTokens, promptCachedInputTokens, promptOutputTokens); ok {
		cost += promptCost
		known = true
	}
	if imageCost, ok := imageCostValue(imageModel, imageTextInputTokens, imageImageInputTokens, imageOutputTokens); ok {
		cost += imageCost
		known = true
	}
	if !known {
		return "-"
	}
	return formatUSD(cost)
}

func imageCostValue(model string, textInputTokens, imageInputTokens, outputTokens int64) (float64, bool) {
	price, ok := imagePrice(model)
	if !ok {
		return 0, false
	}
	cost := float64(textInputTokens)*price.TextInputPerMillion/1_000_000 + float64(imageInputTokens)*price.ImageInputPerMillion/1_000_000 + float64(outputTokens)*price.OutputPerMillion/1_000_000
	return cost, true
}

func imagePrice(model string) (imageTokenPrice, bool) {
	model = strings.TrimSpace(model)
	switch {
	case strings.HasPrefix(model, "gpt-image-1-mini"):
		return imageTokenPrice{TextInputPerMillion: 2.00, ImageInputPerMillion: 2.50, OutputPerMillion: 8.00}, true
	}
	return imageTokenPrice{}, false
}

func formatUSD(cost float64) string {
	if cost <= 0 {
		return "$0.00"
	}
	return fmt.Sprintf("$%.2f", math.Ceil(cost*100)/100)
}

func formatRecordedLLMCost(requests []storage.LLMRequestUsage, inputTokens, cachedInputTokens, outputTokens int64) string {
	var input, cached, output int64
	var cost float64
	for _, usage := range requests {
		input += usage.PromptTokens
		cached += usage.CachedPromptTokens
		output += usage.CompletionTokens
		if usage.PromptTokens == 0 && usage.CompletionTokens == 0 {
			continue
		}
		value, ok := llmCostValue(usage.Model, usage.PromptTokens, usage.CachedPromptTokens, usage.CompletionTokens)
		if !ok {
			return "-"
		}
		cost += value
	}
	// Do not show a misleading partial total if detailed history is missing.
	if input != inputTokens || cached != cachedInputTokens || output != outputTokens {
		return "-"
	}
	return formatUSD(cost)
}
