-- +goose Up
-- +goose StatementBegin
CREATE TABLE dispatch_release_queue (
    assignment_id UUID PRIMARY KEY,
    order_id UUID NOT NULL,
    planned_from TIMESTAMPTZ NOT NULL,
    planned_to TIMESTAMPTZ NOT NULL,
    last_order_id UUID,
    created_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    CHECK (planned_from < planned_to)
);
CREATE INDEX dispatch_release_queue_created_idx
    ON dispatch_release_queue(created_at, assignment_id);

-- Only the latest unsent trigger for an order is needed; sent events are kept.
DELETE FROM integration_outbox WHERE id IN (
    SELECT id FROM (
        SELECT id, row_number() OVER (
            PARTITION BY payload->>'order_id' ORDER BY created_at DESC, id DESC
        ) AS position
        FROM integration_outbox
        WHERE subject = 'dispatch.requested.v1' AND published_at IS NULL
    ) pending WHERE position > 1
);
CREATE UNIQUE INDEX integration_outbox_dispatch_pending_order_idx
    ON integration_outbox ((payload->>'order_id'))
    WHERE subject = 'dispatch.requested.v1' AND published_at IS NULL;
-- +goose StatementEnd

-- +goose Down
-- +goose StatementBegin
DROP INDEX integration_outbox_dispatch_pending_order_idx;
DROP TABLE dispatch_release_queue;
-- +goose StatementEnd
