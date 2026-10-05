-- +goose Up
CREATE INDEX assignments_pending_expiry_idx ON assignments(offer_expires_at, id)
    WHERE status = 'pending_acceptance';

-- +goose Down
DROP INDEX assignments_pending_expiry_idx;
