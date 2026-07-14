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
	Text             string
	PromptTokens     int
	CompletionTokens int
	Duration         time.Duration
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
