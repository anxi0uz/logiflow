package events

import (
	"time"

	"github.com/google/uuid"
)

type DeliveryCompleted struct {
	EventID            uuid.UUID `json:"event_id"`
	OrderID            uuid.UUID `json:"order_id"`
	UserID             uuid.UUID `json:"user_id"`
	OriginAddress      string    `json:"origin_address"`
	DestinationAddress string    `json:"destination_address"`
	CargoDescription   string    `json:"cargo_description"`
	WeightKg           float64   `json:"weight_kg"`
	VolumeM3           float64   `json:"volume_m3"`
	TotalPrice         *float64  `json:"total_price"`
	DeliveredAt        time.Time `json:"delivered_at"`
	RecipientName      *string   `json:"recipient_name"`
	DriverID           uuid.UUID `json:"driver_id"`
	VehicleID          uuid.UUID `json:"vehicle_id"`
}

type DocumentReady struct {
	EventID    uuid.UUID `json:"event_id"`
	DocumentID uuid.UUID `json:"document_id"`
	OrderID    uuid.UUID `json:"order_id"`
	UserID     uuid.UUID `json:"user_id"`
	Type       string    `json:"type"`
}
