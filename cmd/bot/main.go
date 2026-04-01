package main

import (
	"context"
	"errors"
	"log/slog"
	"os"
	"os/signal"
	"syscall"

	"bot-summary-vk/internal/app"
	"bot-summary-vk/internal/config"
	"bot-summary-vk/internal/logging"
)

func main() {
	ctx, stop := signal.NotifyContext(context.Background(), syscall.SIGINT, syscall.SIGTERM)
	defer stop()

	cfg, err := config.Load()
	if err != nil {
		slog.Error("failed to load config", slog.String("error", err.Error()))
		os.Exit(1)
	}

	logger := logging.New(cfg.LogLevel)
	slog.SetDefault(logger)

	application, err := app.New(ctx, cfg, logger)
	if err != nil {
		logger.Error("failed to build application", slog.String("error", err.Error()))
		os.Exit(1)
	}

	if err := application.Run(ctx); err != nil && !errors.Is(err, context.Canceled) {
		logger.Error("application stopped with error", slog.String("error", err.Error()))
		os.Exit(1)
	}

	logger.Info("application stopped")
}
