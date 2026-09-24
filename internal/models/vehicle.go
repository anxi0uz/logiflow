package models

import (
	"time"

	"github.com/google/uuid"
)

type Vehicle struct {
	ID          uuid.UUID `db:"id"`
	PlateNumber string    `db:"plate_number"`
	Brand       *string   `db:"brand"`
	Model       *string   `db:"model"`
	Year        *int      `db:"year"`
	CapacityKg  float64   `db:"capacity_kg"`
	CapacityM3  float64   `db:"capacity_m3"`
	Status      string    `db:"status"` // available, in_transit, maintenance
	Slug        string    `db:"slug"`
}

type VehicleDocument struct {
	ID         uuid.UUID `db:"id"`
	VehicleID  uuid.UUID `db:"vehicle_id"`
	Type       string    `db:"type"`
	Number     string    `db:"number"`
	ValidUntil time.Time `db:"valid_until"`
	Status     string    `db:"status"`
	CreatedAt  time.Time `db:"created_at"`
}
