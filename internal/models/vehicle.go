package models

import "github.com/google/uuid"

type Vehicle struct {
	ID          uuid.UUID `db:"id"`
	PlateNumber string    `db:"plate_number"`
	Brand       string    `db:"brand"`
	Model       string    `db:"model"`
	Year        int       `db:"year"`
	CapacityKg  float64   `db:"capacity_kg"`
	CapacityM3  float64   `db:"capacity_m3"`
	Status      string    `db:"status"` // available, in_transit, maintenance
	Slug        string    `db:"slug"`
}
