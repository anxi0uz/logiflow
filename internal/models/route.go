package models

import (
	"encoding/json"
	"time"

	"github.com/google/uuid"
)

// Coordinate represents a [longitude, latitude] pair — ORS array format.
type Coordinate [2]float64

type Route struct {
	ID           uuid.UUID       `db:"id"`
	OrderID      uuid.UUID       `db:"order_id"`
	DriverID     *uuid.UUID      `db:"driver_id"`
	Coordinates  json.RawMessage `db:"coordinates"` // JSONB: [][2]float64
	CurrentIndex int             `db:"current_index"`
	StartedAt    *time.Time      `db:"started_at"`
	FinishedAt   *time.Time      `db:"finished_at"`
	DistanceKm   float64         `db:"distance_km"`
	DurationSec  int             `db:"duration_sec"`
	Status       string          `db:"status"` // pending, active, finished
}

// ParseCoordinates unmarshals the raw JSONB coordinates into a typed slice.
func (r *Route) ParseCoordinates() ([]Coordinate, error) {
	var coords []Coordinate
	if err := json.Unmarshal(r.Coordinates, &coords); err != nil {
		return nil, err
	}
	return coords, nil
}
