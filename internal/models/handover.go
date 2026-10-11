package models

import (
	"github.com/google/uuid"
	"time"
)

// TripHandover records a request and both drivers' acknowledgements. The old
// assignment remains authoritative until the incoming driver accepts atomically.
type TripHandover struct {
	ID                   uuid.UUID  `db:"id" json:"id"`
	OrderID              uuid.UUID  `db:"order_id" json:"orderId"`
	PreviousAssignmentID uuid.UUID  `db:"previous_assignment_id" json:"previousAssignmentId"`
	NextAssignmentID     *uuid.UUID `db:"next_assignment_id" json:"nextAssignmentId,omitempty"`
	DriverID             uuid.UUID  `db:"driver_id" json:"driverId"`
	VehicleID            uuid.UUID  `db:"vehicle_id" json:"vehicleId"`
	CreatedByUserID      uuid.UUID  `db:"created_by_user_id" json:"createdByUserId"`
	Reason               string     `db:"reason" json:"reason"`
	Location             string     `db:"location" json:"location"`
	CargoCondition       string     `db:"cargo_condition" json:"cargoCondition"`
	PlannedTo            time.Time  `db:"planned_to" json:"plannedTo"`
	Status               string     `db:"status" json:"status"`
	CreatedAt            time.Time  `db:"created_at" json:"createdAt"`
	ExpiresAt            time.Time  `db:"expires_at" json:"expiresAt"`
	OutgoingConfirmedAt  *time.Time `db:"outgoing_confirmed_at" json:"outgoingConfirmedAt,omitempty"`
	CompletedAt          *time.Time `db:"completed_at" json:"completedAt,omitempty"`
}
