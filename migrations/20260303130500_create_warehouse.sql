-- +goose Up
-- +goose StatementBegin
CREATE TABLE IF NOT EXISTS warehouses (
    id          UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    slug        VARCHAR(120) UNIQUE NOT NULL,
    name        VARCHAR(150) NOT NULL,
    address     VARCHAR(255) NOT NULL,
    city        VARCHAR(100),
    latitude    NUMERIC(9,6),
    longitude   NUMERIC(9,6),
    status      VARCHAR(20) DEFAULT 'active',
    created_at  TIMESTAMPTZ DEFAULT NOW()
);
-- +goose StatementEnd

-- +goose Down
-- +goose StatementBegin
DROP TABLE IF EXISTS warehouses;
-- +goose StatementEnd
