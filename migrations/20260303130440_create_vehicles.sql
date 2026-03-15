-- +goose Up
-- +goose StatementBegin
CREATE TABLE IF NOT EXISTS vehicles (
    id              UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    plate_number    VARCHAR(20) UNIQUE NOT NULL,
    brand           VARCHAR(50),
    model           VARCHAR(50),
    year            INTEGER,
    capacity_kg     NUMERIC(10,2),
    capacity_m3     NUMERIC(10,2),
    status          VARCHAR(20) DEFAULT 'available',
    slug    VARCHAR(120) UNIQUE NOT NULL
);


CREATE INDEX idx_vehicles_slug ON vehicles(slug);
-- +goose StatementEnd

-- +goose Down
-- +goose StatementBegin
DROP TABLE IF EXISTS vehicles;
-- +goose StatementEnd
