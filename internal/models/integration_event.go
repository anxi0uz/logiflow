package models

import (
	"encoding/json"
	"time"

	"github.com/google/uuid"
)

type IntegrationEvent struct {
	ID          uuid.UUID       `db:"id"`
	Subject     string          `db:"subject"`
	Payload     json.RawMessage `db:"payload"`
	CreatedAt   time.Time       `db:"created_at"`
	PublishedAt *time.Time      `db:"published_at"`
}
