package llm

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"math"
	"net"
	"net/http"
	"strings"
	"time"

	"bot-summary-vk/internal/config"
)

type OpenAICompatClient struct {
	cfg        config.LLMConfig
	httpClient *http.Client
	logger     *slog.Logger
}

type openAICompatRequest struct {
	Model               string                 `json:"model"`
	Temperature         float64                `json:"temperature,omitempty"`
	MaxTokens           int                    `json:"max_tokens,omitempty"`
	MaxCompletionTokens int                    `json:"max_completion_tokens,omitempty"`
	Messages            []openAICompatMessage  `json:"messages"`
	IncludeReasoning    *bool                  `json:"include_reasoning,omitempty"`
	ReasoningEffort     string                 `json:"reasoning_effort,omitempty"`
	Reasoning           *openAICompatReasoning `json:"reasoning,omitempty"`
}

type openAICompatMessage struct {
	Role    string `json:"role"`
	Content string `json:"content"`
}

type openAICompatReasoning struct {
	Exclude bool `json:"exclude,omitempty"`
}

type openAICompatResponse struct {
	Choices []struct {
		FinishReason string `json:"finish_reason"`
		Message      struct {
			Role             string  `json:"role"`
			Content          *string `json:"content"`
			Reasoning        string  `json:"reasoning,omitempty"`
			ReasoningContent string  `json:"reasoning_content,omitempty"`
		} `json:"message"`
	} `json:"choices"`
	Usage *struct {
		PromptTokens        int `json:"prompt_tokens"`
		CompletionTokens    int `json:"completion_tokens"`
		PromptTokensDetails *struct {
			CachedTokens int `json:"cached_tokens"`
		} `json:"prompt_tokens_details,omitempty"`
	} `json:"usage,omitempty"`
	Error *struct {
		Message string `json:"message"`
	} `json:"error,omitempty"`
}

func NewOpenAICompatClient(cfg config.LLMConfig, httpClient *http.Client, logger *slog.Logger) *OpenAICompatClient {
	return &OpenAICompatClient{cfg: cfg, httpClient: httpClient, logger: logger}
}

func (c *OpenAICompatClient) Provider() string {
	return "openai_compat"
}

func (c *OpenAICompatClient) GenerateSummary(ctx context.Context, input GenerateSummaryInput) (GenerateSummaryOutput, error) {
	payload := openAICompatRequest{
		Model:       c.cfg.Model,
		Temperature: c.cfg.Temperature,
		Messages: []openAICompatMessage{
			{Role: "system", Content: input.SystemPrompt},
			{Role: "user", Content: input.UserPrompt},
		},
	}
	if strings.Contains(c.cfg.BaseURL, "api.openai.com") {
		payload.MaxCompletionTokens = input.MaxOutputTokens
	} else {
		payload.MaxTokens = input.MaxOutputTokens
	}

	if strings.Contains(c.cfg.BaseURL, "openrouter.ai") {
		includeReasoning := false
		payload.IncludeReasoning = &includeReasoning
		payload.Reasoning = &openAICompatReasoning{Exclude: true}
	}
	if strings.Contains(c.cfg.BaseURL, "fireworks.ai") {
		payload.ReasoningEffort = "none"
	}
	if strings.Contains(c.cfg.BaseURL, "ai.api.cloud.yandex.net") {
		if strings.Contains(c.cfg.Model, "gpt-oss") {
			payload.ReasoningEffort = "medium"
		} else if strings.Contains(c.cfg.Model, "qwen") {
			payload.ReasoningEffort = "none"
		}
	}
	if strings.Contains(c.cfg.BaseURL, "api.cloudflare.com") && strings.Contains(c.cfg.Model, "glm-") {
		payload.ReasoningEffort = "low"
	}

	var lastErr error
	for attempt := 0; attempt <= c.cfg.MaxRetries; attempt++ {
		output, retryable, err := c.doRequest(ctx, payload)
		if err == nil {
			return output, nil
		}
		lastErr = err
		if !retryable || attempt == c.cfg.MaxRetries {
			break
		}

		delay := time.Duration(math.Pow(2, float64(attempt))) * c.cfg.RetryBaseDelay
		c.logger.Warn("llm request failed, retrying", slog.Int("attempt", attempt+1), slog.Duration("delay", delay), slog.String("error", err.Error()))

		select {
		case <-ctx.Done():
			return GenerateSummaryOutput{}, ctx.Err()
		case <-time.After(delay):
		}
	}

	return GenerateSummaryOutput{}, fmt.Errorf("generate summary via llm: %w", lastErr)
}

func (c *OpenAICompatClient) doRequest(ctx context.Context, payload openAICompatRequest) (GenerateSummaryOutput, bool, error) {
	startedAt := time.Now()
	body, err := json.Marshal(payload)
	if err != nil {
		return GenerateSummaryOutput{}, false, fmt.Errorf("marshal llm request: %w", err)
	}

	request, err := http.NewRequestWithContext(ctx, http.MethodPost, c.cfg.BaseURL+"/chat/completions", bytes.NewReader(body))
	if err != nil {
		return GenerateSummaryOutput{}, false, fmt.Errorf("create llm request: %w", err)
	}
	request.Header.Set("Authorization", "Bearer "+c.cfg.APIKey)
	request.Header.Set("Content-Type", "application/json")

	response, err := c.httpClient.Do(request)
	if err != nil {
		return GenerateSummaryOutput{}, isTemporaryNetError(err), fmt.Errorf("perform llm request: %w", err)
	}
	defer response.Body.Close()

	responseBody, err := io.ReadAll(io.LimitReader(response.Body, 1<<20))
	if err != nil {
		return GenerateSummaryOutput{}, response.StatusCode >= 500, fmt.Errorf("read llm response: %w", err)
	}
	if response.StatusCode == http.StatusTooManyRequests {
		return GenerateSummaryOutput{}, true, &RateLimitError{Message: (&HTTPStatusError{StatusCode: response.StatusCode, Message: llmErrorMessage(response.StatusCode, responseBody)}).Error()}
	}
	if response.StatusCode >= 500 {
		return GenerateSummaryOutput{}, true, &HTTPStatusError{StatusCode: response.StatusCode, Message: llmErrorMessage(response.StatusCode, responseBody)}
	}
	if response.StatusCode < 200 || response.StatusCode >= 300 {
		return GenerateSummaryOutput{}, false, &HTTPStatusError{StatusCode: response.StatusCode, Message: llmErrorMessage(response.StatusCode, responseBody)}
	}

	var parsed openAICompatResponse
	if err := json.Unmarshal(responseBody, &parsed); err != nil {
		return GenerateSummaryOutput{}, false, fmt.Errorf("decode llm response: %w", err)
	}
	if parsed.Error != nil {
		return GenerateSummaryOutput{}, false, fmt.Errorf("llm error: %s", parsed.Error.Message)
	}
	if len(parsed.Choices) == 0 {
		return GenerateSummaryOutput{}, false, fmt.Errorf("llm returned no choices")
	}

	text := ""
	if parsed.Choices[0].Message.Content != nil {
		text = strings.TrimSpace(*parsed.Choices[0].Message.Content)
	}
	usage := llmUsage(parsed)
	text = stripLeakedAnalysis(text)
	if text == "" {
		choice := parsed.Choices[0]
		reasoningLen := len(choice.Message.Reasoning)
		if len(choice.Message.ReasoningContent) > reasoningLen {
			reasoningLen = len(choice.Message.ReasoningContent)
		}
		return GenerateSummaryOutput{}, false, fmt.Errorf("llm returned empty summary (finish_reason=%q, completion_tokens=%d, reasoning_chars=%d)", choice.FinishReason, usage.CompletionTokens, reasoningLen)
	}
	return GenerateSummaryOutput{
		Text:               text,
		PromptTokens:       usage.PromptTokens,
		CachedPromptTokens: usage.CachedPromptTokens,
		CompletionTokens:   usage.CompletionTokens,
		Duration:           time.Since(startedAt),
	}, false, nil
}

func llmErrorMessage(statusCode int, body []byte) string {
	var parsed struct {
		Error *struct {
			Message string `json:"message"`
			Code    string `json:"code"`
			Param   string `json:"param"`
		} `json:"error"`
	}
	if err := json.Unmarshal(body, &parsed); err == nil && parsed.Error != nil {
		message := strings.TrimSpace(parsed.Error.Message)
		if message != "" {
			return message
		}
	}
	if text := strings.TrimSpace(string(body)); text != "" {
		return compactLLMErrorText(text, 240)
	}
	return http.StatusText(statusCode)
}

func compactLLMErrorText(text string, limit int) string {
	text = strings.Join(strings.Fields(text), " ")
	if limit <= 0 || len(text) <= limit {
		return text
	}
	if limit <= 3 {
		return text[:limit]
	}
	return text[:limit-3] + "..."
}

func llmUsage(parsed openAICompatResponse) GenerateSummaryOutput {
	if parsed.Usage == nil {
		return GenerateSummaryOutput{}
	}
	output := GenerateSummaryOutput{
		PromptTokens:     parsed.Usage.PromptTokens,
		CompletionTokens: parsed.Usage.CompletionTokens,
	}
	if parsed.Usage.PromptTokensDetails != nil {
		output.CachedPromptTokens = parsed.Usage.PromptTokensDetails.CachedTokens
	}
	return output
}

func isTemporaryNetError(err error) bool {
	var netErr net.Error
	if errors.As(err, &netErr) && (netErr.Timeout() || netErr.Temporary()) {
		return true
	}

	text := strings.ToLower(err.Error())
	if strings.Contains(text, "stream error") && strings.Contains(text, "internal_error") {
		return true
	}
	if strings.Contains(text, "http2") && strings.Contains(text, "internal_error") {
		return true
	}

	return false
}

func stripLeakedAnalysis(text string) string {
	text = strings.TrimSpace(text)
	if text == "" {
		return ""
	}

	paragraphs := splitParagraphs(text)
	firstSummaryParagraph := -1
	for i, paragraph := range paragraphs {
		if looksLikeLeakedAnalysis(paragraph) {
			continue
		}
		if hasEnoughCyrillic(paragraph, 3) {
			firstSummaryParagraph = i
			break
		}
	}
	if firstSummaryParagraph <= 0 {
		return text
	}

	return strings.TrimSpace(strings.Join(paragraphs[firstSummaryParagraph:], "\n\n"))
}

func splitParagraphs(text string) []string {
	parts := strings.Split(text, "\n\n")
	result := make([]string, 0, len(parts))
	for _, part := range parts {
		part = strings.TrimSpace(part)
		if part == "" {
			continue
		}
		result = append(result, part)
	}
	return result
}

func looksLikeLeakedAnalysis(text string) bool {
	lower := strings.ToLower(strings.TrimSpace(text))
	markers := []string{
		"let me analyze",
		"key events in this batch",
		"so the story continues",
		"let me write this up",
		"let me draft",
		"i'll analyze",
		"i will analyze",
		"draft:",
		"analysis:",
	}
	for _, marker := range markers {
		if strings.Contains(lower, marker) {
			return true
		}
	}
	return false
}

func hasEnoughCyrillic(text string, minCount int) bool {
	count := 0
	for _, r := range text {
		if r >= 'А' && r <= 'я' || r == 'Ё' || r == 'ё' {
			count++
			if count >= minCount {
				return true
			}
		}
	}
	return false
}
