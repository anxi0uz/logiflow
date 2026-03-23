-- +goose Up
-- +goose StatementBegin
CREATE TABLE IF NOT EXISTS routes (
    id              UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    order_id        UUID UNIQUE REFERENCES orders(id) ON DELETE CASCADE,
    driver_id       UUID REFERENCES drivers(id) ON DELETE SET NULL,
    coordinates     JSONB,
    current_index   INTEGER DEFAULT 0,
    started_at      TIMESTAMPTZ,
    finished_at     TIMESTAMPTZ,
    distance_km     NUMERIC(10,2),
    duration_sec    INTEGER,
    status          VARCHAR(20) DEFAULT 'pending'
);
-- +goose StatementEnd

-- +goose Down
-- +goose StatementBegin
DROP TABLE IF EXISTS routes;
-- +goose StatementEnd
