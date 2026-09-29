-- +goose Up
CREATE TABLE dispatch_recommendations (
    order_id UUID PRIMARY KEY REFERENCES orders(id) ON DELETE CASCADE,
    event_id UUID NOT NULL,
    submitted_at TIMESTAMPTZ NOT NULL,
    requested_at TIMESTAMPTZ NOT NULL,
    created_at TIMESTAMPTZ NOT NULL,
    candidates JSONB NOT NULL DEFAULT '[]'::jsonb
);

-- +goose Down
DROP TABLE dispatch_recommendations;
