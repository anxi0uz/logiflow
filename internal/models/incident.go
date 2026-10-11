package models

import (
	"time"

	"github.com/google/uuid"
)

type OrderIncident struct {
	ID                uuid.UUID  `db:"id" json:"id"`
	OrderID           uuid.UUID  `db:"order_id" json:"orderId"`
	AssignmentID      *uuid.UUID `db:"assignment_id" json:"assignmentId,omitempty"`
	WarehouseID       *uuid.UUID `db:"warehouse_id" json:"warehouseId,omitempty"`
	SourceOperationID *uuid.UUID `db:"source_operation_id" json:"sourceOperationId,omitempty"`
	Kind              string     `db:"kind" json:"kind"`
	Description       string     `db:"description" json:"description"`
	ExpectedPackages  *int       `db:"expected_packages" json:"expectedPackages,omitempty"`
	ActualPackages    *int       `db:"actual_packages" json:"actualPackages,omitempty"`
	Status            string     `db:"status" json:"status"`
	ReportedByUserID  uuid.UUID  `db:"reported_by_user_id" json:"reportedByUserId"`
	ReportedAt        time.Time  `db:"reported_at" json:"reportedAt"`
	Resolution        string     `db:"resolution" json:"resolution"`
	ResolutionNote    string     `db:"resolution_note" json:"resolutionNote"`
	RecipientName     string     `db:"recipient_name" json:"recipientName"`
	ResolvedByUserID  *uuid.UUID `db:"resolved_by_user_id" json:"resolvedByUserId,omitempty"`
	ResolvedAt        *time.Time `db:"resolved_at" json:"resolvedAt,omitempty"`
}
