package services

import (
	"context"
	"encoding/json"
	"fmt"
	"time"

	"github.com/anxi0uz/logiflow/internal/events"
	"github.com/anxi0uz/logiflow/internal/models"
	storage "github.com/anxi0uz/logiflow/pkg"
	"github.com/google/uuid"
	"github.com/huandu/go-sqlbuilder"
	"github.com/jackc/pgx/v5"
)

// buildDispatchRequest takes a best-effort snapshot. Assignment remains the
// authority: candidates can become unavailable immediately after this read.
func buildDispatchRequest(ctx context.Context, tx pgx.Tx, order *models.Order) (events.DispatchRequest, error) {
	request := events.DispatchRequest{
		EventID: uuid.New(), OrderID: order.ID, SubmittedAt: *order.SubmittedAt,
		RequestedAt: time.Now().UTC().Truncate(time.Microsecond),
		PlannedFrom: *order.PickupFrom, PlannedTo: *order.PickupTo,
		WeightKg: order.WeightKg, VolumeM3: order.VolumeM3,
		Drivers: []events.DispatchDriver{}, Vehicles: []events.DispatchVehicle{},
	}
	rows, err := tx.Query(ctx, `SELECT d.id, LEAST(GREATEST(COALESCE(d.rating, 0), 0), 5) FROM drivers d
		WHERE d.status = 'available'
		AND (EXISTS (SELECT 1 FROM driver_documents dd WHERE dd.driver_id = d.id
			AND dd.type = 'license' AND dd.status = 'valid' AND dd.valid_until >= $2::timestamptz::date)
			OR (NOT EXISTS (SELECT 1 FROM driver_documents dd WHERE dd.driver_id = d.id AND dd.type = 'license')
				AND d.license_expiry >= $2::timestamptz::date))
		AND (NOT EXISTS (SELECT 1 FROM driver_shifts ds WHERE ds.driver_id = d.id)
			OR EXISTS (SELECT 1 FROM driver_shifts ds WHERE ds.driver_id = d.id
				AND ds.starts_at <= $1 AND ds.ends_at >= $2))
		AND NOT EXISTS (SELECT 1 FROM assignments a WHERE a.driver_id = d.id
			AND a.status IN ('pending_acceptance', 'accepted', 'active')
			AND tstzrange(a.planned_from, a.planned_to, '[)') && tstzrange($1, $2, '[)'))
		ORDER BY d.rating DESC, d.id LIMIT 30`, request.PlannedFrom, request.PlannedTo)
	if err != nil {
		return request, fmt.Errorf("dispatch drivers: %w", err)
	}
	for rows.Next() {
		var driver events.DispatchDriver
		if err := rows.Scan(&driver.ID, &driver.Rating); err != nil {
			rows.Close()
			return request, err
		}
		request.Drivers = append(request.Drivers, driver)
	}
	if err := rows.Err(); err != nil {
		rows.Close()
		return request, err
	}
	rows.Close()
	rows, err = tx.Query(ctx, `SELECT v.id, v.capacity_kg, v.capacity_m3 FROM vehicles v
		WHERE v.status = 'available' AND v.capacity_kg >= $3 AND v.capacity_m3 >= $4
		AND EXISTS (SELECT 1 FROM vehicle_documents vd WHERE vd.vehicle_id = v.id
			AND vd.type = 'registration' AND vd.status = 'valid' AND vd.valid_until >= $2::timestamptz::date)
		AND NOT EXISTS (SELECT 1 FROM assignments a WHERE a.vehicle_id = v.id
			AND a.status IN ('pending_acceptance', 'accepted', 'active')
			AND tstzrange(a.planned_from, a.planned_to, '[)') && tstzrange($1, $2, '[)'))
		ORDER BY v.capacity_kg, v.id LIMIT 30`, request.PlannedFrom, request.PlannedTo, request.WeightKg, request.VolumeM3)
	if err != nil {
		return request, fmt.Errorf("dispatch vehicles: %w", err)
	}
	for rows.Next() {
		var vehicle events.DispatchVehicle
		if err := rows.Scan(&vehicle.ID, &vehicle.CapacityKg, &vehicle.CapacityM3); err != nil {
			rows.Close()
			return request, err
		}
		request.Vehicles = append(request.Vehicles, vehicle)
	}
	if err := rows.Err(); err != nil {
		rows.Close()
		return request, err
	}
	rows.Close()
	return request, nil
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
	if _, err := s.GetOrder(ctx, orderID, userID, role); err != nil {
		return nil, err
	}
	tx, err := s.db.Begin(ctx)
	if err != nil {
		return nil, err
	}
	defer tx.Rollback(ctx) //nolint:errcheck
	order, err := storage.GetOne[models.Order](ctx, tx, "orders", func(sb *sqlbuilder.SelectBuilder) {
		sb.Where(sb.EQ("id", orderID)).ForUpdate()
	})
	if err != nil {
		return nil, err
	}
	if role == "manager" {
		warehouseID, err := s.managerWarehouseID(ctx, tx, userID)
		if err != nil {
			return nil, err
		}
		if order.OriginWarehouseID == nil || *order.OriginWarehouseID != warehouseID {
			return nil, ErrForbidden
		}
	}
	if order.Status != models.OrderReadyForDispatch || order.SubmittedAt == nil {
		return nil, ErrInvalidOrderTransition
	}
	request, err := buildDispatchRequest(ctx, tx, order)
	if err != nil {
		return nil, err
	}
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
