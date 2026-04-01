package app

import (
	"context"
	"fmt"
	"log/slog"
	"net/http"
	"time"

	"bot-summary-vk/internal/config"
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
}

func New(ctx context.Context, cfg config.Config, logger *slog.Logger) (*App, error) {
	repo, err := storage.New(ctx, cfg.DatabaseURL, cfg.DatabaseMaxConns, cfg.DatabaseMinConns, cfg.DatabaseConnectTimeout, cfg.DatabaseQueryTimeout)
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

	vkClient := vk.NewClient(vkHTTPClient, cfg.VK)
	consumer := vk.NewLongPollConsumer(vkClient, longPollHTTPClient, cfg.VK.LongPollWait, logger)
	llmClient, err := llm.New(cfg.LLM, llmHTTPClient, logger)
	if err != nil {
		repo.Close()
		return nil, fmt.Errorf("init llm client: %w", err)
	}

	summaryService := summary.NewService(repo, llmClient, vkClient, cfg, logger)
	ingestion := NewMessageIngestionService(repo, cfg.Manual, vkClient, summaryService, vkClient, cfg.LLM.RequestTimeout+30*time.Second, logger)

	logger.Info("application initialized",
		slog.Bool("process_all_group_chats", true),
		slog.String("llm_provider", llmClient.Provider()),
		slog.Duration("vk_api_timeout", cfg.VK.RequestTimeout),
		slog.Duration("vk_longpoll_http_timeout", longPollTimeout),
	)

	return &App{repo: repo, consumer: consumer, ingestion: ingestion}, nil
}

func (a *App) Run(ctx context.Context) error {
	defer a.repo.Close()

	group, groupCtx := errgroup.WithContext(ctx)
	group.Go(func() error {
		return a.consumer.Run(groupCtx, a.ingestion.HandleMessage)
	})

	if err := group.Wait(); err != nil && err != context.Canceled {
		return err
	}
	return nil
}
