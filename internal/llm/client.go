package llm

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"net/http"
	"time"

	"bot-summary-vk/internal/config"
)

type Client interface {
	GenerateSummary(ctx context.Context, input GenerateSummaryInput) (GenerateSummaryOutput, error)
	Provider() string
}

type GenerateSummaryInput struct {
	SystemPrompt    string
	UserPrompt      string
	MaxOutputTokens int
}

type GenerateSummaryOutput struct {
	Text               string
	PromptTokens       int
	CachedPromptTokens int
	CompletionTokens   int
	Duration           time.Duration
}

type RateLimitError struct {
	Message string
}

func (e *RateLimitError) Error() string {
	if e == nil || e.Message == "" {
		return "llm rate limited"
	}
	return e.Message
}

type HTTPStatusError struct {
	StatusCode int
	Message    string
}

func (e *HTTPStatusError) Error() string {
	if e == nil {
		return "llm returned http error"
	}
	message := e.PublicMessage()
	if message == "" {
		return fmt.Sprintf("llm returned status %d", e.StatusCode)
	}
	return fmt.Sprintf("llm returned status %d: %s", e.StatusCode, message)
}

func (e *HTTPStatusError) PublicMessage() string {
	if e == nil {
		return ""
	}
	if e.Message != "" {
		return e.Message
	}
	return http.StatusText(e.StatusCode)
}

func IsRateLimited(err error) bool {
	var rateLimitErr *RateLimitError
	return errors.As(err, &rateLimitErr)
}

func New(cfg config.LLMConfig, httpClient *http.Client, logger *slog.Logger) (Client, error) {
	switch cfg.Provider {
	case "stub":
		return NewStubClient(cfg.Model), nil
	case "openai_compat":
		return NewOpenAICompatClient(cfg, httpClient, logger), nil
	default:
		return nil, fmt.Errorf("unsupported LLM_PROVIDER %q", cfg.Provider)
	}
}
