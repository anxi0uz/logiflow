package models

import (
	"time"

	"github.com/google/uuid"
)

type Order struct {
	ID                 uuid.UUID  `db:"id"`
	CreatedByID        *uuid.UUID `db:"created_by_id"`
	DriverID           *uuid.UUID `db:"driver_id"`
	ManagerID          *uuid.UUID `db:"manager_id"`
	OriginWarehouseID  *uuid.UUID `db:"origin_warehouse_id"`
	OriginAddress      string     `db:"origin_address"`
	DestinationAddress string     `db:"destination_address"`
	CargoDescription   string     `db:"cargo_description"`
	WeightKg           float64    `db:"weight_kg"`
	VolumeM3           float64    `db:"volume_m3"`
	Status             string     `db:"status"` // pending, assigned, in_transit, delivered, cancelled
	TotalPrice         float64    `db:"total_price"`
	CreatedAt          time.Time  `db:"created_at"`
	AssignedAt         *time.Time `db:"assigned_at"`
	DeliveredAt        *time.Time `db:"delivered_at"`
}
