-- +goose Up
ALTER TABLE warehouse_operations DROP CONSTRAINT warehouse_operations_operation_type_check;
ALTER TABLE warehouse_operations DROP CONSTRAINT warehouse_operations_package_count_check;
ALTER TABLE warehouse_operations DROP CONSTRAINT warehouse_operations_check;
ALTER TABLE warehouse_operations ADD COLUMN recipient_name TEXT NOT NULL DEFAULT '' CHECK (length(recipient_name) <= 200);
ALTER TABLE warehouse_operations ADD CONSTRAINT warehouse_operations_operation_type_check CHECK (operation_type IN ('receive','load','unload','undo_load','return_to_sender'));
ALTER TABLE warehouse_operations ADD CONSTRAINT warehouse_operations_package_count_check CHECK (package_count >= 0 AND (operation_type NOT IN ('receive','load') OR package_count > 0));
ALTER TABLE warehouse_operations ADD CONSTRAINT warehouse_operations_check CHECK ((operation_type IN ('receive','return_to_sender') AND assignment_id IS NULL) OR (operation_type IN ('load','unload','undo_load') AND assignment_id IS NOT NULL));
CREATE TABLE order_incidents (
    id UUID PRIMARY KEY,
    order_id UUID NOT NULL REFERENCES orders(id) ON DELETE RESTRICT,
    assignment_id UUID REFERENCES assignments(id) ON DELETE RESTRICT,
    warehouse_id UUID REFERENCES warehouses(id) ON DELETE RESTRICT,
    source_operation_id UUID REFERENCES warehouse_operations(id) ON DELETE RESTRICT,
    kind TEXT NOT NULL CHECK (kind IN ('vehicle_breakdown','cargo_damage','quantity_discrepancy','recipient_refusal','other')),
    description TEXT NOT NULL CHECK (length(btrim(description)) BETWEEN 1 AND 2000),
    expected_packages INTEGER CHECK (expected_packages >= 0),
    actual_packages INTEGER CHECK (actual_packages >= 0),
    status TEXT NOT NULL CHECK (status IN ('open','resolved')),
    reported_by_user_id UUID NOT NULL REFERENCES users(id) ON DELETE RESTRICT,
    reported_at TIMESTAMPTZ NOT NULL,
    resolution TEXT NOT NULL DEFAULT '',
    resolution_note TEXT NOT NULL DEFAULT '' CHECK (length(resolution_note) <= 2000),
    recipient_name TEXT NOT NULL DEFAULT '' CHECK (length(recipient_name) <= 200),
    resolved_by_user_id UUID REFERENCES users(id) ON DELETE RESTRICT,
    resolved_at TIMESTAMPTZ,
    CHECK ((status='open' AND resolved_at IS NULL AND resolved_by_user_id IS NULL AND resolution='') OR (status='resolved' AND resolved_at IS NOT NULL AND resolved_by_user_id IS NOT NULL AND length(btrim(resolution_note))>0 AND resolution<>'')),
    CHECK (kind <> 'quantity_discrepancy' OR (expected_packages IS NOT NULL AND actual_packages IS NOT NULL AND expected_packages <> actual_packages)),
    UNIQUE (source_operation_id, kind)
);
CREATE INDEX order_incidents_history ON order_incidents(order_id, reported_at, id);
CREATE INDEX order_incidents_open ON order_incidents(order_id) WHERE status='open';
-- +goose Down
DROP TABLE order_incidents;
-- Refuse a destructive downgrade if the new operational history exists.
ALTER TABLE warehouse_operations DROP CONSTRAINT warehouse_operations_operation_type_check;
ALTER TABLE warehouse_operations ADD CONSTRAINT warehouse_operations_operation_type_check CHECK (operation_type IN ('receive','load','unload'));
ALTER TABLE warehouse_operations DROP CONSTRAINT warehouse_operations_package_count_check;
ALTER TABLE warehouse_operations ADD CONSTRAINT warehouse_operations_package_count_check CHECK (package_count > 0);
ALTER TABLE warehouse_operations DROP CONSTRAINT warehouse_operations_check;
ALTER TABLE warehouse_operations ADD CONSTRAINT warehouse_operations_check CHECK ((operation_type='receive' AND assignment_id IS NULL) OR (operation_type IN ('load','unload') AND assignment_id IS NOT NULL));
ALTER TABLE warehouse_operations DROP COLUMN recipient_name;
