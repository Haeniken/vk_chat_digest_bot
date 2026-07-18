package app

import (
	"context"
	"fmt"
	"log/slog"
	"net/http"
	"time"

	"bot-summary-vk/internal/config"
	"bot-summary-vk/internal/imagegen"
	"bot-summary-vk/internal/llm"
	"bot-summary-vk/internal/storage"
	"bot-summary-vk/internal/summary"
	"bot-summary-vk/internal/vk"

	"golang.org/x/sync/errgroup"
)

type App struct {
	repo      *storage.Repository
	consumer  *vk.LongPollConsumer
	ingestion *MessageIngestionService
	logger    *slog.Logger
}

func New(ctx context.Context, cfg config.Config, logger *slog.Logger) (*App, error) {
	repo, err := storage.New(ctx, cfg.DatabaseURL, cfg.DatabaseMaxConns, cfg.DatabaseMinConns, cfg.DatabaseConnectTimeout, cfg.DatabaseQueryTimeout, time.Duration(cfg.Summary.HistoryRetentionDays)*24*time.Hour, time.Duration(cfg.Summary.MessageRetentionDays)*24*time.Hour)
	if err != nil {
		return nil, fmt.Errorf("init storage: %w", err)
	}

	vkHTTPClient := &http.Client{Timeout: cfg.VK.RequestTimeout}
	longPollTimeout := cfg.VK.RequestTimeout
	minLongPollTimeout := time.Duration(cfg.VK.LongPollWait+10) * time.Second
	if longPollTimeout < minLongPollTimeout {
		longPollTimeout = minLongPollTimeout
	}
	longPollHTTPClient := &http.Client{Timeout: longPollTimeout}
	llmHTTPClient := &http.Client{Timeout: cfg.LLM.RequestTimeout}
	imageHTTPClient := &http.Client{Timeout: cfg.Image.Timeout + 15*time.Second}
	imagePromptLLMHTTPClient := &http.Client{Timeout: cfg.ImagePromptLLM.RequestTimeout}

	vkClient := vk.NewClient(vkHTTPClient, cfg.VK)
	consumer := vk.NewLongPollConsumer(vkClient, longPollHTTPClient, cfg.VK.LongPollWait, logger)
	llmClient, err := llm.New(cfg.LLM, llmHTTPClient, logger)
	if err != nil {
		repo.Close()
		return nil, fmt.Errorf("init llm client: %w", err)
	}

	var imagePromptLLMClient llm.Client
	var imageGenerator summary.ImageGenerator
	if cfg.Image.Enabled {
		imagePromptLLMClient, err = llm.New(cfg.ImagePromptLLM, imagePromptLLMHTTPClient, logger)
		if err != nil {
			repo.Close()
			return nil, fmt.Errorf("init image prompt llm client: %w", err)
		}
		switch cfg.Image.Provider {
		case "cloudflare":
			imageGenerator = imagegen.NewCloudflareClient(cfg.Image, imageHTTPClient)
		case "openai":
			imageGenerator = imagegen.NewOpenAIClient(cfg.Image, imageHTTPClient)
		default:
			imageGenerator = imagegen.NewYandexARTClient(cfg.Image, imageHTTPClient)
		}
	}

	summaryService := summary.NewService(repo, llmClient, imagePromptLLMClient, vkClient, cfg, logger, imageGenerator)
	manualExecutionTimeout := cfg.LLM.RequestTimeout + 30*time.Second
	if cfg.Image.Enabled {
		manualExecutionTimeout += cfg.ImagePromptLLM.RequestTimeout + cfg.Image.Timeout + 30*time.Second
	}
	ingestion := NewMessageIngestionService(repo, cfg.Manual, vkClient, vkClient, summaryService, cfg.LLM.Model, cfg.ImagePromptLLM.Model, cfg.Image.Model, vkClient, manualExecutionTimeout, logger)

	logger.Info("application initialized",
		slog.Bool("process_all_group_chats", true),
		slog.String("llm_provider", llmClient.Provider()),
		slog.Bool("summary_image_enabled", cfg.Image.Enabled),
		slog.String("summary_image_provider", cfg.Image.Provider),
		slog.String("summary_image_model", cfg.Image.Model),
		slog.String("summary_image_prompt_llm_model", cfg.ImagePromptLLM.Model),
		slog.Duration("vk_api_timeout", cfg.VK.RequestTimeout),
		slog.Duration("vk_longpoll_http_timeout", longPollTimeout),
	)

	return &App{repo: repo, consumer: consumer, ingestion: ingestion, logger: logger}, nil
}

func (a *App) Run(ctx context.Context) error {
	defer a.repo.Close()

	group, groupCtx := errgroup.WithContext(ctx)
	a.ingestion.Start(groupCtx)
	defer a.ingestion.StopAndWait()

	group.Go(func() error {
		return a.consumer.Run(groupCtx, a.ingestion.HandleMessage, a.ingestion.HandleMessageEvent)
	})
	group.Go(func() error {
		return a.runRetentionCleanup(groupCtx)
	})

	if err := group.Wait(); err != nil && err != context.Canceled {
		return err
	}
	return nil
}

func (a *App) runRetentionCleanup(ctx context.Context) error {
	a.pruneExpiredData(ctx)

	ticker := time.NewTicker(24 * time.Hour)
	defer ticker.Stop()

	for {
		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-ticker.C:
			a.pruneExpiredData(ctx)
		}
	}
}

func (a *App) pruneExpiredData(ctx context.Context) {
	if err := a.repo.PruneExpiredData(ctx); err != nil {
		if ctx.Err() != nil {
			return
		}
		a.logger.Warn("retention cleanup failed", slog.String("error", err.Error()))
		return
	}
	a.logger.Debug("retention cleanup completed")
}
