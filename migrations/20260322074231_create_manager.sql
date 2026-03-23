-- +goose Up
-- +goose StatementBegin
CREATE TABLE IF NOT EXISTS managers (
    id           UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    user_id      UUID UNIQUE REFERENCES users(id) ON DELETE CASCADE,
    warehouse_id UUID REFERENCES warehouses(id) ON DELETE SET NULL,
    slug         VARCHAR(120) UNIQUE NOT NULL
);
-- +goose StatementEnd

-- +goose Down
-- +goose StatementBegin
DROP TABLE IF EXISTS managers;
-- +goose StatementEnd
