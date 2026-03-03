package database

import (
	"context"
	"log/slog"

	"github.com/jackc/pgx/v5/pgxpool"
)

func NewConnectionPool(ctx context.Context, connectionString string) (*pgxpool.Pool, error) {
	pool, err := pgxpool.New(ctx, connectionString)
	if err != nil {
		slog.ErrorContext(ctx, "Cant create pool of connection", slog.String("Error", err.Error()))
		return nil, err
	}

	if err := pool.Ping(ctx); err != nil {
		slog.ErrorContext(ctx, "Cant ping connection", slog.String("Error", err.Error()))
		return nil, err
	}

	return pool, nil
}
