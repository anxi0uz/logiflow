package models

import (
	"time"

	"github.com/google/uuid"
)

type Warehouse struct {
	ID        uuid.UUID `db:"id"`
	Slug      string    `db:"slug"`
	Name      string    `db:"name"`
	Address   string    `db:"address"`
	City      string    `db:"city"`
	Latitude  float64   `db:"latitude"`
	Longitude float64   `db:"longitude"`
	Status    string    `db:"status"` // active, inactive
	CreatedAt time.Time `db:"created_at"`
}
