package database

import (
	"context"
	"log/slog"

	"github.com/anxi0uz/logiflow/internal/config"
	"github.com/redis/go-redis"
	"github.com/redis/go-redis/v9"
)

func NewRedisConnection(ctx context.Context, cfg *config.Config) (*redis.Client, error) {
	rdb := redis.NewClient(&redis.Options{
		Addr:     cfg.Redis.Addr,
		Password: cfg.Redis.Password,
		DB:       cfg.Redis.DB,
	})

	_, err := rdb.Ping().Result()
	if err != nil {
		slog.ErrorContext(ctx, "Cant ping redis", slog.String("Error", err.Error()))
		return nil, err
	}

	return rdb, nil
}
