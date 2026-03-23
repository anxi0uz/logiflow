-- +goose Up
-- +goose StatementBegin
CREATE TABLE IF NOT EXISTS orders (
    id                  UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    created_by_id       UUID REFERENCES users(id) ON DELETE SET NULL,
    driver_id           UUID REFERENCES drivers(id) ON DELETE SET NULL,
    manager_id          UUID REFERENCES managers(id) ON DELETE SET NULL,
    origin_warehouse_id UUID REFERENCES warehouses(id) ON DELETE SET NULL,
    origin_address      VARCHAR(255),
    destination_address VARCHAR(255) NOT NULL,
    cargo_description   TEXT,
    weight_kg           NUMERIC(10,2),
    volume_m3           NUMERIC(10,2),
    status              VARCHAR(20) DEFAULT 'pending',
    total_price         NUMERIC(12,2),
    created_at          TIMESTAMPTZ DEFAULT NOW(),
    assigned_at         TIMESTAMPTZ,
    delivered_at        TIMESTAMPTZ
);
-- +goose StatementEnd

-- +goose Down
-- +goose StatementBegin
DROP TABLE IF EXISTS orders;
-- +goose StatementEnd
