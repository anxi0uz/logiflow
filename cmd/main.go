package main

import (
	"context"
	"log/slog"
	"os"
	"os/signal"
	"syscall"

	"github.com/anxi0uz/logiflow/internal/config"
	"github.com/anxi0uz/logiflow/internal/database"
	"github.com/anxi0uz/logiflow/internal/handler"
	"github.com/golang-cz/devslog"
)

func NewDevLogger() {
	opts := &devslog.Options{
		MaxSlicePrintSize: 4,
		SortKeys:          true,
		TimeFormat:        "15:04:05.000",
		NewLineAfterLog:   true,
		DebugColor:        devslog.Cyan,
		StringerFormatter: true,
	}

	handler := devslog.NewHandler(os.Stdout, opts)
	logger := slog.New(handler)

	slog.SetDefault(logger)
}

func main() {
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	NewDevLogger()

	cfg, err := config.NewConfig(ctx, "configs/config.toml")
	if err != nil {
		slog.ErrorContext(ctx, "Cant load configs", slog.String("Error", err.Error()))
		os.Exit(1)
	}

	slog.SetLogLoggerLevel(cfg.Logiflow.LogLevel)
	connectionPool, err := database.NewConnectionPool(ctx, cfg.DatabaseURL())
	if err != nil {
		slog.ErrorContext(ctx, "Ошибка подключения к postgres", slog.String("error", err.Error()))
		os.Exit(1)
	}
	defer connectionPool.Close()

	redis, err := database.NewRedisConnection(ctx, cfg)
	if err != nil {
		slog.ErrorContext(ctx, "Ошибка подключения к redis", slog.String("error", err.Error()))
		os.Exit(1)
	}
	defer redis.Close()

	if err := database.RunMigrations(ctx, cfg.DatabaseURL()); err != nil {
		slog.ErrorContext(ctx, "Ошибка миграций", slog.String("error", err.Error()))
		os.Exit(1)
	}

	sigChan := make(chan os.Signal, 1)
	signal.Notify(sigChan, syscall.SIGINT, syscall.SIGTERM, syscall.SIGHUP)

	serverErr := make(chan error, 1)
	go func() {
		serverErr <- handler.NewServer(connectionPool, redis, cfg).Run()
	}()

	select {
	case sig := <-sigChan:
		slog.Info("Получен сигнал завершения", "signal", sig.String())
	case err := <-serverErr:
		slog.Error("Сервер упал", "error", err.Error())
		os.Exit(1)
	}
	cancel()

	slog.Info("Приложение остановлено")
}
