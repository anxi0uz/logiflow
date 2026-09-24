-- +goose Up
-- +goose StatementBegin
CREATE TABLE integration_outbox (
    id UUID PRIMARY KEY,
    subject VARCHAR(128) NOT NULL,
    payload JSONB NOT NULL,
    created_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    published_at TIMESTAMPTZ
);
CREATE INDEX integration_outbox_pending_idx ON integration_outbox(created_at, id)
    WHERE published_at IS NULL;

CREATE TABLE integration_inbox (
    event_id UUID PRIMARY KEY,
    processed_at TIMESTAMPTZ NOT NULL DEFAULT NOW()
);

ALTER TABLE notifications ADD COLUMN document_id UUID;
-- +goose StatementEnd

-- +goose Down
-- +goose StatementBegin
ALTER TABLE notifications DROP COLUMN document_id;
DROP TABLE integration_inbox;
DROP TABLE integration_outbox;
-- +goose StatementEnd
