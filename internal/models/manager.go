package models

import "github.com/google/uuid"

type Manager struct {
	ID          uuid.UUID  `db:"id"`
	UserID      uuid.UUID  `db:"user_id"`
	WarehouseID *uuid.UUID `db:"warehouse_id"`
	Slug        string     `db:"slug"`
}
