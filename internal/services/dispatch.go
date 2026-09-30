package services

import (
	"context"
	"encoding/json"
	"time"

	"github.com/anxi0uz/logiflow/internal/events"
	"github.com/anxi0uz/logiflow/internal/models"
	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
)

func buildDispatchRequest(order *models.Order) events.DispatchRequest {
	return events.DispatchRequest{
		EventID: uuid.New(), OrderID: order.ID, SubmittedAt: order.SubmittedAt.UTC().Truncate(time.Microsecond),
		RequestedAt: time.Now().UTC().Truncate(time.Microsecond),
	}
}

type DispatchRecommendations struct {
	Status     string                     `json:"status"`
	CreatedAt  *time.Time                 `json:"createdAt,omitempty"`
	Candidates []events.DispatchCandidate `json:"candidates"`
}

func (s *OrderService) BuildDispatchRequest(ctx context.Context, orderID, userID uuid.UUID, role string) (*events.DispatchRequest, error) {
	if role != "manager" && role != "admin" {
		return nil, ErrForbidden
	}
	order, err := s.GetOrder(ctx, orderID, userID, role)
	if err != nil {
		return nil, err
	}
	if order.Status != models.OrderReadyForDispatch || order.SubmittedAt == nil {
		return nil, ErrInvalidOrderTransition
	}
	request := buildDispatchRequest(order)
	return &request, nil
}

func (s *OrderService) GetDispatchRecommendations(ctx context.Context, orderID, userID uuid.UUID, role string) (*DispatchRecommendations, error) {
	if role != "manager" && role != "admin" {
		return nil, ErrForbidden
	}
	order, err := s.GetOrder(ctx, orderID, userID, role)
	if err != nil {
		return nil, err
	}
	result := &DispatchRecommendations{Status: "pending", Candidates: []events.DispatchCandidate{}}
	if order.Status != models.OrderReadyForDispatch {
		result.Status = "unavailable"
		return result, nil
	}
	var submittedAt time.Time
	var createdAt time.Time
	var candidates []byte
	err = s.db.QueryRow(ctx, `SELECT submitted_at, created_at, candidates FROM dispatch_recommendations WHERE order_id = $1`, orderID).
		Scan(&submittedAt, &createdAt, &candidates)
	if err == pgx.ErrNoRows {
		return result, nil
	}
	if err != nil {
		return nil, err
	}
	if !submittedAt.Equal(*order.SubmittedAt) {
		return result, nil
	}
	if err := json.Unmarshal(candidates, &result.Candidates); err != nil {
		return nil, err
	}
	result.Status = "ready"
	result.CreatedAt = &createdAt
	return result, nil
}
