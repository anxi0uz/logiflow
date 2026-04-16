package storage

import (
	"context"
	"errors"
	"log/slog"

	"github.com/huandu/go-sqlbuilder"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
)

type Querier interface {
	Query(context.Context, string, ...any) (pgx.Rows, error)
	QueryRow(context.Context, string, ...any) pgx.Row
	Exec(context.Context, string, ...any) (pgconn.CommandTag, error)
}

var ErrNotFound = errors.New("not found")

func GetAll[T any](ctx context.Context, table string, db Querier, opts ...func(*sqlbuilder.SelectBuilder)) ([]T, error) {
	sb := sqlbuilder.NewStruct(new(T)).For(sqlbuilder.PostgreSQL).SelectFrom(table)

	sb.From(table)

	for _, opt := range opts {
		opt(sb)
	}

	query, args := sb.Build()

	rows, err := db.Query(ctx, query, args...)
	if err != nil {
		slog.ErrorContext(ctx, "cannot execute get all query",
			slog.String("query", query),
			slog.Any("args", args),
			slog.String("error", err.Error()),
		)
		return nil, err
	}
	defer rows.Close()

	lists, err := pgx.CollectRows(rows, pgx.RowToStructByNameLax[T])
	if err != nil {
		slog.ErrorContext(ctx, "cannot scan courses",
			slog.String("query", query),
			slog.String("error", err.Error()),
		)
		return nil, err
	}

	return lists, nil
}

func GetOne[T any](ctx context.Context, db Querier, table string, opts ...func(*sqlbuilder.SelectBuilder)) (*T, error) {
	itemsStruct := sqlbuilder.NewStruct(new(T)).For(sqlbuilder.PostgreSQL)

	sb := itemsStruct.SelectFrom(table)

	for _, opt := range opts {
		opt(sb)
	}

	query, args := sb.Build()

	rows, err := db.Query(ctx, query, args...)
	if err != nil {
		slog.ErrorContext(ctx, "query failed", "query", query, "args", args, "err", err)
		return nil, err
	}
	defer rows.Close()

	item, err := pgx.CollectOneRow(rows, pgx.RowToStructByNameLax[T])
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return nil, ErrNotFound
		}
		slog.ErrorContext(ctx, "cannot collect row", "query", query, "args", args, "err", err)
		return nil, err
	}

	return &item, nil
}

func Create[T any](ctx context.Context, table string, item T, db Querier, opts ...func(*sqlbuilder.SelectBuilder)) error {
	structs := sqlbuilder.NewStruct(new(T))

	sb := structs.WithoutTag("db", "-").InsertInto(table, item)
	sb.SetFlavor(sqlbuilder.PostgreSQL)

	query, args := sb.Build()

	if _, err := db.Exec(ctx, query, args...); err != nil {
		slog.ErrorContext(ctx, "cannot create item",
			slog.String("query", query),
			slog.Any("args", args),
			slog.String("error", err.Error()),
		)
		return err
	}

	return nil
}

func Update[T any](ctx context.Context, table string, item T, db Querier, opts ...func(*sqlbuilder.UpdateBuilder)) error {
	structs := sqlbuilder.NewStruct(new(T))

	sb := structs.WithoutTag("db", "-").WithoutTag("immutable").Update(table, item)
	sb.SetFlavor(sqlbuilder.PostgreSQL)

	for _, opt := range opts {
		opt(sb)
	}

	query, args := sb.Build()

	if _, err := db.Exec(ctx, query, args...); err != nil {
		slog.ErrorContext(ctx, "cannot update item",
			slog.String("query", query),
			slog.Any("args", args),
			slog.String("error", err.Error()),
		)
		return err
	}

	return nil
}

func Delete[T any](ctx context.Context, table string, db Querier, opts ...func(*sqlbuilder.DeleteBuilder)) error {
	structs := sqlbuilder.NewStruct(new(T))

	sb := structs.WithoutTag("db", "-").DeleteFrom(table)
	sb.SetFlavor(sqlbuilder.PostgreSQL)

	for _, opt := range opts {
		opt(sb)
	}

	query, args := sb.Build()

	if _, err := db.Exec(ctx, query, args...); err != nil {
		slog.ErrorContext(ctx, "cannot delete item",
			slog.String("query", query),
			slog.Any("args", args),
			slog.String("error", err.Error()),
		)
		return err
	}

	return nil
}
