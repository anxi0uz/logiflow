-- +goose Up
CREATE TABLE counterparties (
    id UUID PRIMARY KEY,
    owner_user_id UUID NOT NULL REFERENCES users(id) ON DELETE RESTRICT,
    kind TEXT NOT NULL CHECK (kind IN ('organization','entrepreneur','individual')),
    name TEXT NOT NULL CHECK (length(btrim(name)) BETWEEN 1 AND 300),
    inn TEXT NOT NULL DEFAULT '',
    kpp TEXT NOT NULL DEFAULT '',
    address TEXT NOT NULL CHECK (length(btrim(address)) BETWEEN 1 AND 1000),
    contact_name TEXT NOT NULL CHECK (length(btrim(contact_name)) BETWEEN 1 AND 200),
    phone TEXT NOT NULL DEFAULT '' CHECK (length(phone) <= 40),
    email TEXT NOT NULL DEFAULT '' CHECK (length(email) <= 254),
    shared BOOLEAN NOT NULL DEFAULT false,
    status TEXT NOT NULL CHECK (status IN ('active','archived')),
    created_at TIMESTAMPTZ NOT NULL,
    updated_at TIMESTAMPTZ NOT NULL,
    CHECK ((kind='organization' AND inn ~ '^[0-9]{10}$' AND (kpp='' OR kpp ~ '^[0-9]{9}$')) OR
           (kind='entrepreneur' AND inn ~ '^[0-9]{12}$' AND kpp='') OR
           (kind='individual' AND (inn='' OR inn ~ '^[0-9]{12}$') AND kpp=''))
);
CREATE INDEX counterparties_owner ON counterparties(owner_user_id, created_at, id);
ALTER TABLE orders ADD COLUMN transport_policy_version SMALLINT NOT NULL DEFAULT 0 CHECK (transport_policy_version IN (0,1));
ALTER TABLE orders ADD COLUMN transport_details JSONB CHECK (transport_details IS NULL OR jsonb_typeof(transport_details)='object');
CREATE TABLE delivery_receipts (
    id UUID PRIMARY KEY,
    order_id UUID NOT NULL UNIQUE REFERENCES orders(id) ON DELETE RESTRICT,
    assignment_id UUID NOT NULL UNIQUE REFERENCES assignments(id) ON DELETE RESTRICT,
    recipient_name TEXT NOT NULL CHECK (length(btrim(recipient_name)) BETWEEN 1 AND 200),
    recipient_role TEXT NOT NULL CHECK (length(btrim(recipient_role)) BETWEEN 1 AND 200),
    authority_basis TEXT NOT NULL CHECK (length(btrim(authority_basis)) BETWEEN 1 AND 1000),
    package_count INTEGER NOT NULL CHECK (package_count >= 0),
    cargo_condition TEXT NOT NULL CHECK (length(btrim(cargo_condition)) BETWEEN 1 AND 2000),
    seal_number TEXT NOT NULL DEFAULT '' CHECK (length(seal_number) <= 100),
    comment TEXT NOT NULL DEFAULT '' CHECK (length(comment) <= 2000),
    received_at TIMESTAMPTZ NOT NULL,
    recorded_at TIMESTAMPTZ NOT NULL,
    recorded_by_user_id UUID NOT NULL REFERENCES users(id) ON DELETE RESTRICT
);
-- +goose Down
DROP TABLE delivery_receipts;
ALTER TABLE orders DROP COLUMN transport_details;
ALTER TABLE orders DROP COLUMN transport_policy_version;
DROP TABLE counterparties;
