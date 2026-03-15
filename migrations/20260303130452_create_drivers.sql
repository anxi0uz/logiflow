-- +goose Up
-- +goose StatementBegin
CREATE TABLE IF NOT EXISTS drivers(
    id UUID         PRIMARY KEY DEFAULT gen_random_uuid(),
    user_id UUID    REFERENCES users(id) ON DELETE CASCADE,
    vehicle_id UUID REFERENCES vehicles(id) ON DELETE SET NULL,
    license_number  VARCHAR (50) NOT NULL,
    license_expiry  DATE NOT NULL,
    rating          NUMERIC(3,2) DEFAULT 5.00,
    slug            VARCHAR(120) UNIQUE NOT NULL,
    status          VARCHAR(20) DEFAULT 'available'
);
-- +goose StatementEnd

-- +goose Down
-- +goose StatementBegin
DROP TABLE IF EXISTS drivers;
-- +goose StatementEnd
