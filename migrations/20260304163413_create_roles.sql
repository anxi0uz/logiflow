-- +goose Up
-- +goose StatementBegin
CREATE TABLE IF NOT EXISTS roles(
    id      UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    title   VARCHAR(20),
    code    VARCHAR(20)
);

-- +goose StatementEnd

-- +goose Down
-- +goose StatementBegin
DROP TABLE IF EXISTS roles;
-- +goose StatementEnd
