package events

import (
	"context"
	"encoding/json"
	"fmt"
	"math"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
)

func receiveDispatchRecommended(ctx context.Context, db *pgxpool.Pool, data []byte) error {
	return StoreDispatchRecommended(ctx, db, data)
}

// StoreDispatchRecommended is shared by the NATS consumer and the synchronous refresh.
func StoreDispatchRecommended(ctx context.Context, db *pgxpool.Pool, data []byte) error {
	var event DispatchRecommended
	if err := json.Unmarshal(data, &event); err != nil {
		return invalidEventError{fmt.Sprintf("decode dispatch recommendation: %v", err)}
	}
	if event.EventID == uuid.Nil || event.OrderID == uuid.Nil || event.SubmittedAt.IsZero() ||
		event.RequestedAt.IsZero() || event.CreatedAt.IsZero() || len(event.Candidates) > 5 {
		return invalidEventError{"invalid dispatch recommendation identifiers or timestamps"}
	}
	for _, candidate := range event.Candidates {
		if _, err := uuid.Parse(candidate.ID); err != nil || candidate.DriverID == uuid.Nil ||
			candidate.VehicleID == uuid.Nil || math.IsNaN(candidate.Score) || math.IsInf(candidate.Score, 0) {
			return invalidEventError{"invalid dispatch candidate"}
		}
	}
	candidates, err := json.Marshal(event.Candidates)
	if err != nil {
		return err
	}
	tx, err := db.Begin(ctx)
	if err != nil {
		return err
	}
	defer tx.Rollback(ctx) //nolint:errcheck
	inserted, err := tx.Exec(ctx, `INSERT INTO integration_inbox(event_id) VALUES($1) ON CONFLICT DO NOTHING`, event.EventID)
	if err != nil {
		return err
	}
	if inserted.RowsAffected() == 0 {
		return tx.Commit(ctx)
	}
	var status string
	var submittedAt time.Time
	err = tx.QueryRow(ctx, `SELECT status, submitted_at FROM orders WHERE id = $1 FOR UPDATE`, event.OrderID).Scan(&status, &submittedAt)
	if err == pgx.ErrNoRows {
		return tx.Commit(ctx)
	}
	if err != nil {
		return err
	}
	if status == "ready_for_dispatch" && submittedAt.Equal(event.SubmittedAt) {
		_, err = tx.Exec(ctx, `INSERT INTO dispatch_recommendations
			(order_id, event_id, submitted_at, requested_at, created_at, candidates)
			VALUES ($1, $2, $3, $4, $5, $6)
			ON CONFLICT (order_id) DO UPDATE SET
				event_id = EXCLUDED.event_id,
				submitted_at = EXCLUDED.submitted_at,
				requested_at = EXCLUDED.requested_at,
				created_at = EXCLUDED.created_at,
				candidates = EXCLUDED.candidates
			WHERE dispatch_recommendations.requested_at <= EXCLUDED.requested_at`,
			event.OrderID, event.EventID, event.SubmittedAt, event.RequestedAt, event.CreatedAt, candidates)
		if err != nil {
			return err
		}
	}
	return tx.Commit(ctx)
}
