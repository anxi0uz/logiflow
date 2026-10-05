package services

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"time"

	"github.com/anxi0uz/logiflow/internal/models"
	storage "github.com/anxi0uz/logiflow/pkg"
	"github.com/google/uuid"
	"github.com/huandu/go-sqlbuilder"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
)

const assignmentExpiryInterval = 30 * time.Second
const assignmentExpiryBatchSize = 100

type assignmentExpiryCursor struct {
	expiresAt time.Time
	id        uuid.UUID
}

// RunAssignmentExpiry sweeps immediately on startup, then periodically. It is
// independent of NATS: expiry and refresh triggers commit to PostgreSQL first.
func (s *OrderService) RunAssignmentExpiry(ctx context.Context) {
	ticker := time.NewTicker(assignmentExpiryInterval)
	defer ticker.Stop()
	var cursor *assignmentExpiryCursor
	for ctx.Err() == nil {
		sweepCtx, cancel := context.WithTimeout(ctx, assignmentExpiryInterval)
		err := s.expireDueAssignmentsAfter(sweepCtx, &cursor)
		cancel()
		if err != nil && ctx.Err() == nil {
			slog.ErrorContext(ctx, "assignment expiry sweep failed", slog.String("error", err.Error()))
		}
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
		}
	}
}

func (s *OrderService) expireDueAssignments(ctx context.Context) error {
	var cursor *assignmentExpiryCursor
	return s.expireDueAssignmentsAfter(ctx, &cursor)
}

func (s *OrderService) expireDueAssignmentsAfter(ctx context.Context, cursor **assignmentExpiryCursor) error {
	stale, err := storage.GetAll[models.Assignment](ctx, "assignments", s.db, func(sb *sqlbuilder.SelectBuilder) {
		sb.Where(sb.EQ("status", models.AssignmentPendingAcceptance), sb.LE("offer_expires_at", time.Now())).
			OrderBy("offer_expires_at", "id").Limit(assignmentExpiryBatchSize)
		if *cursor != nil {
			sb.Where(sb.Or(sb.G("offer_expires_at", (*cursor).expiresAt),
				sb.And(sb.EQ("offer_expires_at", (*cursor).expiresAt), sb.G("id", (*cursor).id))))
		}
	})
	if err != nil {
		return err
	}
	var failures []error
	for _, candidate := range stale {
		if err := ctx.Err(); err != nil {
			return errors.Join(append(failures, err)...)
		}
		if err := s.expireAssignmentByID(ctx, candidate, false); err != nil && !assignmentLockBusy(err) && !errors.Is(err, storage.ErrNotFound) {
			failures = append(failures, err)
		}
		// Advance even for busy/failed candidates so they cannot starve later pages.
		*cursor = &assignmentExpiryCursor{expiresAt: candidate.OfferExpiresAt, id: candidate.ID}
	}
	if len(stale) < assignmentExpiryBatchSize {
		*cursor = nil
	}
	return errors.Join(failures...)
}

func assignmentLockBusy(err error) bool {
	var pgErr *pgconn.PgError
	return errors.As(err, &pgErr) && pgErr.Code == "55P03"
}

func (s *OrderService) expireAssignments(ctx context.Context, candidates []models.Assignment) error {
	for _, candidate := range candidates {
		if err := s.expireAssignmentByID(ctx, candidate, true); err != nil {
			return err
		}
	}
	return nil
}

func (s *OrderService) expireAssignmentByID(ctx context.Context, candidate models.Assignment, wait bool) error {
	tx, err := s.db.Begin(ctx)
	if err != nil {
		return err
	}
	defer tx.Rollback(ctx) //nolint:errcheck
	_, assignment, _, _, err := s.lockAssignmentContextWithWait(ctx, tx, candidate.ID, wait)
	if err != nil {
		return err
	}
	// Re-read under the same locks as accept/reject/reassignment. Another worker
	// or command may have already closed or accepted the discovered offer.
	if err := s.expireAssignment(ctx, tx, assignment, time.Now()); err != nil {
		return err
	}
	return tx.Commit(ctx)
}

func (s *OrderService) expireAssignment(ctx context.Context, tx pgx.Tx, assignment *models.Assignment, now time.Time) error {
	if assignment.Status != models.AssignmentPendingAcceptance || now.Before(assignment.OfferExpiresAt) {
		return nil
	}
	assignment.Status = models.AssignmentExpired
	if err := storage.Update(ctx, "assignments", *assignment, tx, func(ub *sqlbuilder.UpdateBuilder) {
		ub.Where(ub.EQ("id", assignment.ID))
	}); err != nil {
		return err
	}
	if err := s.notify(ctx, tx, assignment.CreatedByUserID, "Предложение истекло",
		fmt.Sprintf("Назначение %s по заказу %s не принято вовремя", assignment.ID, assignment.OrderID)); err != nil {
		return err
	}
	return s.enqueueDispatchAfterRelease(ctx, tx, assignment)
}
