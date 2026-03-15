package main

import (
	"context"
	"log/slog"
	"os"

	"github.com/anxi0uz/logiflow/internal/config"
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
	}

	slog.SetLogLoggerLevel(cfg.Logiflow.LogLevel)
}
