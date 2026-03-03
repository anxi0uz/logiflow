-- +goose Up
-- +goose StatementBegin
CREATE EXTENSION IF NOT EXISTS pg_tgrm;
-- +goose StatementEnd

-- +goose Down
-- +goose StatementBegin
DROP EXTENSION IF EXISTS pg_tgrm;
-- +goose StatementEnd
