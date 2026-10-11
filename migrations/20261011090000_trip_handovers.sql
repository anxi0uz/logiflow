-- +goose Up
-- +goose StatementBegin
CREATE TABLE trip_handovers (
    id UUID PRIMARY KEY,
    order_id UUID NOT NULL REFERENCES orders(id) ON DELETE RESTRICT,
    previous_assignment_id UUID NOT NULL REFERENCES assignments(id) ON DELETE RESTRICT,
    next_assignment_id UUID REFERENCES assignments(id) ON DELETE RESTRICT,
    driver_id UUID NOT NULL REFERENCES drivers(id) ON DELETE RESTRICT,
    vehicle_id UUID NOT NULL REFERENCES vehicles(id) ON DELETE RESTRICT,
    created_by_user_id UUID NOT NULL REFERENCES users(id) ON DELETE RESTRICT,
    reason TEXT NOT NULL CHECK (length(btrim(reason)) BETWEEN 1 AND 1000),
    location TEXT NOT NULL CHECK (length(btrim(location)) BETWEEN 1 AND 1000),
    cargo_condition TEXT NOT NULL CHECK (length(btrim(cargo_condition)) BETWEEN 1 AND 2000),
    planned_to TIMESTAMPTZ NOT NULL,
    status VARCHAR(20) NOT NULL DEFAULT 'pending'
        CHECK (status IN ('pending', 'completed', 'cancelled', 'expired')),
    created_at TIMESTAMPTZ NOT NULL,
    expires_at TIMESTAMPTZ NOT NULL CHECK (expires_at > created_at),
    outgoing_confirmed_at TIMESTAMPTZ,
    completed_at TIMESTAMPTZ,
    CHECK (status <> 'completed' OR (next_assignment_id IS NOT NULL
        AND outgoing_confirmed_at IS NOT NULL AND completed_at IS NOT NULL))
);
CREATE UNIQUE INDEX trip_handovers_one_pending ON trip_handovers(order_id) WHERE status = 'pending';
CREATE INDEX trip_handovers_order_history ON trip_handovers(order_id, created_at, id);
-- +goose StatementEnd

-- +goose Down
DROP TABLE trip_handovers;
