-- +goose Up
-- +goose StatementBegin
CREATE EXTENSION IF NOT EXISTS btree_gist;

ALTER TABLE orders
    ALTER COLUMN status SET DEFAULT 'draft',
    ADD COLUMN IF NOT EXISTS pickup_from TIMESTAMPTZ,
    ADD COLUMN IF NOT EXISTS pickup_to TIMESTAMPTZ,
    ADD COLUMN IF NOT EXISTS submitted_at TIMESTAMPTZ,
    ADD COLUMN IF NOT EXISTS started_at TIMESTAMPTZ,
    ADD COLUMN IF NOT EXISTS arrived_at TIMESTAMPTZ,
    ADD COLUMN IF NOT EXISTS cancelled_at TIMESTAMPTZ;

UPDATE orders SET status = 'draft' WHERE status = 'pending';
UPDATE orders SET status = 'completed' WHERE status = 'delivered';

ALTER TABLE orders
    ADD CONSTRAINT orders_status_check
        CHECK (status IN ('draft', 'ready_for_dispatch', 'assigned', 'in_transit', 'arrived', 'completed', 'cancelled')),
    ADD CONSTRAINT orders_weight_check CHECK (weight_kg IS NULL OR weight_kg >= 0),
    ADD CONSTRAINT orders_volume_check CHECK (volume_m3 IS NULL OR volume_m3 >= 0),
    ADD CONSTRAINT orders_pickup_window_check CHECK (pickup_from IS NULL OR pickup_to IS NULL OR pickup_from < pickup_to),
    ADD CONSTRAINT orders_completed_at_check CHECK (status <> 'completed' OR delivered_at IS NOT NULL),
    ADD CONSTRAINT orders_cancelled_at_check CHECK (status <> 'cancelled' OR cancelled_at IS NOT NULL);

CREATE TABLE assignments (
    id                        UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    order_id                  UUID NOT NULL REFERENCES orders(id) ON DELETE CASCADE,
    driver_id                 UUID NOT NULL REFERENCES drivers(id) ON DELETE RESTRICT,
    vehicle_id                UUID NOT NULL REFERENCES vehicles(id) ON DELETE RESTRICT,
    status                    VARCHAR(32) NOT NULL DEFAULT 'pending_acceptance',
    planned_from              TIMESTAMPTZ NOT NULL,
    planned_to                TIMESTAMPTZ NOT NULL,
    created_by_user_id        UUID NOT NULL REFERENCES users(id) ON DELETE RESTRICT,
    source                    VARCHAR(32) NOT NULL DEFAULT 'manual',
    recommendation_id         VARCHAR(255),
    recommendation_created_at TIMESTAMPTZ,
    assigned_at               TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    offer_expires_at          TIMESTAMPTZ NOT NULL,
    accepted_at               TIMESTAMPTZ,
    rejected_at               TIMESTAMPTZ,
    released_at               TIMESTAMPTZ,
    started_at                TIMESTAMPTZ,
    completed_at              TIMESTAMPTZ,
    recipient_name            VARCHAR(255),
    delivery_comment          TEXT,
    rejection_reason_code     VARCHAR(64),
    rejection_comment         TEXT,
    release_reason_code       VARCHAR(64),
    CONSTRAINT assignments_status_check CHECK (status IN (
        'pending_acceptance', 'accepted', 'active', 'rejected', 'released', 'expired', 'completed'
    )),
    CONSTRAINT assignments_source_check CHECK (source IN ('manual', 'dispatch_recommendation')),
    CONSTRAINT assignments_window_check CHECK (planned_from < planned_to),
    CONSTRAINT assignments_offer_check CHECK (assigned_at < offer_expires_at),
    CONSTRAINT assignments_accepted_at_check CHECK (status NOT IN ('accepted', 'active', 'completed') OR accepted_at IS NOT NULL),
    CONSTRAINT assignments_started_at_check CHECK (status NOT IN ('active', 'completed') OR started_at IS NOT NULL),
    CONSTRAINT assignments_completed_at_check CHECK (status <> 'completed' OR completed_at IS NOT NULL),
    CONSTRAINT assignments_rejected_at_check CHECK (status <> 'rejected' OR rejected_at IS NOT NULL),
    CONSTRAINT assignments_released_at_check CHECK (status <> 'released' OR released_at IS NOT NULL)
);

CREATE UNIQUE INDEX assignments_one_open_per_order
    ON assignments(order_id)
    WHERE status IN ('pending_acceptance', 'accepted', 'active');

ALTER TABLE assignments ADD CONSTRAINT assignments_driver_no_overlap
    EXCLUDE USING gist (
        driver_id WITH =,
        tstzrange(planned_from, planned_to, '[)') WITH &&
    ) WHERE (status IN ('pending_acceptance', 'accepted', 'active'));

ALTER TABLE assignments ADD CONSTRAINT assignments_vehicle_no_overlap
    EXCLUDE USING gist (
        vehicle_id WITH =,
        tstzrange(planned_from, planned_to, '[)') WITH &&
    ) WHERE (status IN ('pending_acceptance', 'accepted', 'active'));

CREATE INDEX assignments_order_idx ON assignments(order_id, assigned_at DESC);
CREATE INDEX assignments_driver_idx ON assignments(driver_id, planned_from, planned_to);
CREATE INDEX assignments_vehicle_idx ON assignments(vehicle_id, planned_from, planned_to);

CREATE TABLE order_status_history (
    id            UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    order_id      UUID NOT NULL REFERENCES orders(id) ON DELETE CASCADE,
    from_status   VARCHAR(32),
    to_status     VARCHAR(32) NOT NULL,
    actor_user_id UUID REFERENCES users(id) ON DELETE SET NULL,
    reason_code   VARCHAR(64),
    created_at    TIMESTAMPTZ NOT NULL DEFAULT NOW()
);

CREATE INDEX order_status_history_order_idx ON order_status_history(order_id, created_at);

CREATE TABLE driver_documents (
    id          UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    driver_id   UUID NOT NULL REFERENCES drivers(id) ON DELETE CASCADE,
    type        VARCHAR(32) NOT NULL,
    number      VARCHAR(128) NOT NULL,
    valid_until DATE NOT NULL,
    status      VARCHAR(20) NOT NULL DEFAULT 'valid',
    created_at  TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    CONSTRAINT driver_documents_status_check CHECK (status IN ('valid', 'suspended', 'expired')),
    UNIQUE (driver_id, type, number)
);

CREATE INDEX driver_documents_validity_idx ON driver_documents(driver_id, type, status, valid_until);

CREATE TABLE driver_shifts (
    id        UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    driver_id UUID NOT NULL REFERENCES drivers(id) ON DELETE CASCADE,
    starts_at TIMESTAMPTZ NOT NULL,
    ends_at   TIMESTAMPTZ NOT NULL,
    CONSTRAINT driver_shifts_window_check CHECK (starts_at < ends_at)
);

CREATE INDEX driver_shifts_driver_idx ON driver_shifts(driver_id, starts_at, ends_at);

CREATE TABLE vehicle_documents (
    id          UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    vehicle_id  UUID NOT NULL REFERENCES vehicles(id) ON DELETE CASCADE,
    type        VARCHAR(32) NOT NULL,
    number      VARCHAR(128) NOT NULL,
    valid_until DATE NOT NULL,
    status      VARCHAR(20) NOT NULL DEFAULT 'valid',
    created_at  TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    CONSTRAINT vehicle_documents_status_check CHECK (status IN ('valid', 'suspended', 'expired')),
    UNIQUE (vehicle_id, type, number)
);

CREATE INDEX vehicle_documents_validity_idx ON vehicle_documents(vehicle_id, type, status, valid_until);
-- +goose StatementEnd

-- +goose Down
-- +goose StatementBegin
DROP TABLE IF EXISTS vehicle_documents;
DROP TABLE IF EXISTS driver_shifts;
DROP TABLE IF EXISTS driver_documents;
DROP TABLE IF EXISTS order_status_history;
DROP TABLE IF EXISTS assignments;

ALTER TABLE orders
    DROP CONSTRAINT IF EXISTS orders_pickup_window_check,
    DROP CONSTRAINT IF EXISTS orders_volume_check,
    DROP CONSTRAINT IF EXISTS orders_weight_check,
    DROP CONSTRAINT IF EXISTS orders_completed_at_check,
    DROP CONSTRAINT IF EXISTS orders_cancelled_at_check,
    DROP CONSTRAINT IF EXISTS orders_status_check,
    DROP COLUMN IF EXISTS cancelled_at,
    DROP COLUMN IF EXISTS arrived_at,
    DROP COLUMN IF EXISTS started_at,
    DROP COLUMN IF EXISTS submitted_at,
    DROP COLUMN IF EXISTS pickup_to,
    DROP COLUMN IF EXISTS pickup_from,
    ALTER COLUMN status SET DEFAULT 'pending';
-- +goose StatementEnd
