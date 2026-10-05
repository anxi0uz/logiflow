package services

import (
	"context"
	"log/slog"
	"time"

	"github.com/anxi0uz/logiflow/internal/models"
	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
)

const dispatchRefreshInterval = time.Second
const dispatchRefreshBatchSize = 100

// RunDispatchRefresh expands committed releases independently of NATS and HTTP
// commands. Each transaction visits at most one page of affected orders.
func (s *OrderService) RunDispatchRefresh(ctx context.Context) {
	ticker := time.NewTicker(dispatchRefreshInterval)
	defer ticker.Stop()
	for ctx.Err() == nil {
		batchCtx, cancel := context.WithTimeout(ctx, 10*time.Second)
		_, err := s.processDispatchRelease(batchCtx)
		cancel()
		if err != nil && ctx.Err() == nil {
			slog.ErrorContext(ctx, "dispatch refresh expansion failed", slog.String("error", err.Error()))
		}
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
		}
	}
}

func (s *OrderService) processDispatchRelease(ctx context.Context) (bool, error) {
	tx, err := s.db.Begin(ctx)
	if err != nil {
		return false, err
	}
	defer tx.Rollback(ctx) //nolint:errcheck
	var releaseID, orderID uuid.UUID
	var from, to time.Time
	var cursor *uuid.UUID
	err = tx.QueryRow(ctx, `SELECT assignment_id, order_id, planned_from, planned_to, last_order_id
		FROM dispatch_release_queue ORDER BY created_at, assignment_id
		LIMIT 1 FOR UPDATE SKIP LOCKED`).Scan(&releaseID, &orderID, &from, &to, &cursor)
	if err == pgx.ErrNoRows {
		return false, nil
	}
	if err != nil {
		return false, err
	}
	// Read other orders without taking domain locks. Stale triggers are safe;
	// Dispatch and Assignment recheck the order and resource state.
	rows, err := tx.Query(ctx, `SELECT o.id, o.submitted_at FROM orders o
		WHERE o.status = 'ready_for_dispatch' AND o.submitted_at IS NOT NULL
		AND ($4::uuid IS NULL OR o.id > $4)
		AND (o.id = $1 OR (o.pickup_from < $3 AND o.pickup_to > $2))
		AND NOT EXISTS (SELECT 1 FROM assignments a WHERE a.order_id = o.id
			AND a.status IN ('pending_acceptance', 'accepted', 'active'))
		ORDER BY o.id LIMIT $5`, orderID, from, to, cursor, dispatchRefreshBatchSize)
	if err != nil {
		return false, err
	}
	orders, err := pgx.CollectRows(rows, pgx.RowToStructByNameLax[models.Order])
	if err != nil {
		return false, err
	}
	for i := range orders {
		if err := enqueueDispatchRequest(ctx, tx, &orders[i]); err != nil {
			return false, err
		}
	}
	if len(orders) < dispatchRefreshBatchSize {
		_, err = tx.Exec(ctx, `DELETE FROM dispatch_release_queue WHERE assignment_id = $1`, releaseID)
	} else {
		_, err = tx.Exec(ctx, `UPDATE dispatch_release_queue SET last_order_id = $2
			WHERE assignment_id = $1`, releaseID, orders[len(orders)-1].ID)
	}
	if err != nil {
		return false, err
	}
	return true, tx.Commit(ctx)
}
