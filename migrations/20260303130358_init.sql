-- +goose Up
-- +goose StatementBegin
CREATE EXTENSION IF NOT EXISTS pg_trgm;
-- +goose StatementEnd

-- +goose Down
-- +goose StatementBegin
DROP EXTENSION IF EXISTS pg_trgm;
-- +goose StatementEnd
