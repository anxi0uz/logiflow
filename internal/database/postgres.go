package database

import "github.com/jackc/pgx/v5/pgxpool"

func NewConnectionPool(connectionString string) (*pgxpool.Pool, error)
