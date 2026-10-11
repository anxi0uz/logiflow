-- +goose Up
ALTER TABLE orders ADD COLUMN destination_warehouse_id UUID REFERENCES warehouses(id) ON DELETE RESTRICT;
ALTER TABLE orders DROP CONSTRAINT orders_origin_warehouse_id_fkey;
ALTER TABLE orders ADD CONSTRAINT orders_origin_warehouse_id_fkey FOREIGN KEY (origin_warehouse_id) REFERENCES warehouses(id) ON DELETE RESTRICT;
CREATE INDEX orders_destination_warehouse_idx ON orders(destination_warehouse_id);
CREATE TABLE warehouse_operations (
    id UUID PRIMARY KEY,
    order_id UUID NOT NULL REFERENCES orders(id) ON DELETE RESTRICT,
    warehouse_id UUID NOT NULL REFERENCES warehouses(id) ON DELETE RESTRICT,
    assignment_id UUID REFERENCES assignments(id) ON DELETE RESTRICT,
    operation_type TEXT NOT NULL CHECK (operation_type IN ('receive','load','unload')),
    package_count INTEGER NOT NULL CHECK (package_count > 0),
    cargo_condition TEXT NOT NULL CHECK (length(btrim(cargo_condition)) BETWEEN 1 AND 2000),
    comment TEXT NOT NULL DEFAULT '' CHECK (length(comment) <= 2000),
    created_by_user_id UUID NOT NULL REFERENCES users(id) ON DELETE RESTRICT,
    occurred_at TIMESTAMPTZ NOT NULL,
    CHECK ((operation_type = 'receive' AND assignment_id IS NULL) OR (operation_type IN ('load','unload') AND assignment_id IS NOT NULL)),
    UNIQUE NULLS NOT DISTINCT (order_id, operation_type, assignment_id)
);
CREATE INDEX warehouse_operations_journal ON warehouse_operations(warehouse_id, occurred_at, id);
-- +goose Down
DROP TABLE warehouse_operations;
ALTER TABLE orders DROP CONSTRAINT orders_origin_warehouse_id_fkey;
ALTER TABLE orders ADD CONSTRAINT orders_origin_warehouse_id_fkey FOREIGN KEY (origin_warehouse_id) REFERENCES warehouses(id) ON DELETE SET NULL;
ALTER TABLE orders DROP COLUMN destination_warehouse_id;
