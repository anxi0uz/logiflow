package models

import (
	"time"

	"github.com/google/uuid"
)

type Order struct {
	ID                 uuid.UUID  `db:"id" json:"id"`
	CreatedByID        *uuid.UUID `db:"created_by_id" json:"createdById,omitempty"`
	DriverID           *uuid.UUID `db:"driver_id" json:"driverId,omitempty"`
	ManagerID          *uuid.UUID `db:"manager_id" json:"managerId,omitempty"`
	OriginWarehouseID  *uuid.UUID `db:"origin_warehouse_id" json:"originWarehouseId,omitempty"`
	OriginAddress      string     `db:"origin_address" json:"originAddress"`
	DestinationAddress string     `db:"destination_address" json:"destinationAddress"`
	CargoDescription   string     `db:"cargo_description" json:"cargoDescription"`
	WeightKg           float64    `db:"weight_kg" json:"weightKg"`
	VolumeM3           float64    `db:"volume_m3" json:"volumeM3"`
	Status             string     `db:"status" json:"status"`
	TotalPrice         float64    `db:"total_price" json:"totalPrice"`
	PickupFrom         *time.Time `db:"pickup_from" json:"pickupFrom,omitempty"`
	PickupTo           *time.Time `db:"pickup_to" json:"pickupTo,omitempty"`
	CreatedAt          time.Time  `db:"created_at" json:"createdAt"`
	SubmittedAt        *time.Time `db:"submitted_at" json:"submittedAt,omitempty"`
	AssignedAt         *time.Time `db:"assigned_at" json:"assignedAt,omitempty"`
	StartedAt          *time.Time `db:"started_at" json:"startedAt,omitempty"`
	ArrivedAt          *time.Time `db:"arrived_at" json:"arrivedAt,omitempty"`
	DeliveredAt        *time.Time `db:"delivered_at" json:"deliveredAt,omitempty"`
	CancelledAt        *time.Time `db:"cancelled_at" json:"cancelledAt,omitempty"`
}

const (
	OrderDraft            = "draft"
	OrderReadyForDispatch = "ready_for_dispatch"
	OrderAssigned         = "assigned"
	OrderInTransit        = "in_transit"
	OrderArrived          = "arrived"
	OrderCompleted        = "completed"
	OrderCancelled        = "cancelled"
)

func CanTransitionOrder(from, to string) bool {
	switch from {
	case OrderDraft:
		return to == OrderReadyForDispatch || to == OrderCancelled
	case OrderReadyForDispatch:
		return to == OrderAssigned || to == OrderCancelled
	case OrderAssigned:
		return to == OrderReadyForDispatch || to == OrderInTransit || to == OrderCancelled
	case OrderInTransit:
		return to == OrderArrived
	case OrderArrived:
		return to == OrderCompleted
	default:
		return false
	}
}
