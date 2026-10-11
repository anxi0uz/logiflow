package models

import (
	"time"

	"github.com/google/uuid"
)

type WarehouseOperation struct {
	RecipientName   string     `db:"recipient_name" json:"recipientName,omitempty"`
	ID              uuid.UUID  `db:"id" json:"id"`
	OrderID         uuid.UUID  `db:"order_id" json:"orderId"`
	WarehouseID     uuid.UUID  `db:"warehouse_id" json:"warehouseId"`
	AssignmentID    *uuid.UUID `db:"assignment_id" json:"assignmentId,omitempty"`
	OperationType   string     `db:"operation_type" json:"operationType"`
	PackageCount    int        `db:"package_count" json:"packageCount"`
	CargoCondition  string     `db:"cargo_condition" json:"cargoCondition"`
	Comment         string     `db:"comment" json:"comment"`
	CreatedByUserID uuid.UUID  `db:"created_by_user_id" json:"createdByUserId"`
	OccurredAt      time.Time  `db:"occurred_at" json:"occurredAt"`
}

type WarehouseQueueItem struct {
	Order
	WarehouseStage    string `json:"warehouseStage"`
	OpenIncidentCount int    `json:"openIncidentCount"`
}
