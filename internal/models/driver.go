package models

import (
	"time"

	"github.com/google/uuid"
)

type Driver struct {
	ID            uuid.UUID  `db:"id"`
	UserID        uuid.UUID  `db:"user_id"`
	VehicleID     *uuid.UUID `db:"vehicle_id"`
	LicenseNumber string     `db:"license_number"`
	LicenseExpiry time.Time  `db:"license_expiry"`
	Rating        float64    `db:"rating"`
	Slug          string     `db:"slug"`
	Status        string     `db:"status"` // available, on_route, off_duty
}

type DriverDocument struct {
	ID         uuid.UUID `db:"id"`
	DriverID   uuid.UUID `db:"driver_id"`
	Type       string    `db:"type"`
	Number     string    `db:"number"`
	ValidUntil time.Time `db:"valid_until"`
	Status     string    `db:"status"`
	CreatedAt  time.Time `db:"created_at"`
}
