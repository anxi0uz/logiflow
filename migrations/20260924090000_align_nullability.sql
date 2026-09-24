-- +goose Up
-- +goose StatementBegin
-- Optional vehicle metadata and warehouse coordinates remain nullable in Go.
-- Unknown operational state must fail closed; missing capacity cannot satisfy a load.
UPDATE users SET full_name = '' WHERE full_name IS NULL;
UPDATE users SET avatar_url = '' WHERE avatar_url IS NULL;
UPDATE users SET role = 'disabled' WHERE role IS NULL;
UPDATE users SET created_at = NOW() WHERE created_at IS NULL;
UPDATE users SET updated_at = created_at WHERE updated_at IS NULL;
ALTER TABLE users
    ALTER COLUMN full_name SET DEFAULT '', ALTER COLUMN full_name SET NOT NULL,
    ALTER COLUMN avatar_url SET DEFAULT '', ALTER COLUMN avatar_url SET NOT NULL,
    ALTER COLUMN role SET DEFAULT 'disabled', ALTER COLUMN role SET NOT NULL,
    ALTER COLUMN created_at SET NOT NULL,
    ALTER COLUMN updated_at SET DEFAULT NOW(), ALTER COLUMN updated_at SET NOT NULL;

UPDATE vehicles SET capacity_kg = 0 WHERE capacity_kg IS NULL;
UPDATE vehicles SET capacity_m3 = 0 WHERE capacity_m3 IS NULL;
UPDATE vehicles SET status = 'maintenance' WHERE status IS NULL;
ALTER TABLE vehicles
    ALTER COLUMN capacity_kg SET DEFAULT 0, ALTER COLUMN capacity_kg SET NOT NULL,
    ALTER COLUMN capacity_m3 SET DEFAULT 0, ALTER COLUMN capacity_m3 SET NOT NULL,
    ALTER COLUMN status SET DEFAULT 'maintenance', ALTER COLUMN status SET NOT NULL,
    ADD CONSTRAINT vehicles_capacity_nonnegative CHECK (capacity_kg >= 0 AND capacity_m3 >= 0);

UPDATE drivers SET rating = 0 WHERE rating IS NULL;
UPDATE drivers SET status = 'off_duty' WHERE status IS NULL;
ALTER TABLE drivers
    ALTER COLUMN user_id SET NOT NULL,
    ALTER COLUMN rating SET NOT NULL,
    ALTER COLUMN status SET NOT NULL;

UPDATE warehouses SET city = '' WHERE city IS NULL;
UPDATE warehouses SET status = 'inactive' WHERE status IS NULL;
UPDATE warehouses SET created_at = NOW() WHERE created_at IS NULL;
ALTER TABLE warehouses
    ALTER COLUMN city SET DEFAULT '', ALTER COLUMN city SET NOT NULL,
    ALTER COLUMN status SET DEFAULT 'inactive', ALTER COLUMN status SET NOT NULL,
    ALTER COLUMN created_at SET NOT NULL;

ALTER TABLE managers ALTER COLUMN user_id SET NOT NULL;

UPDATE orders SET origin_address = '' WHERE origin_address IS NULL;
UPDATE orders SET cargo_description = '' WHERE cargo_description IS NULL;
UPDATE orders SET weight_kg = 0 WHERE weight_kg IS NULL;
UPDATE orders SET volume_m3 = 0 WHERE volume_m3 IS NULL;
UPDATE orders SET status = 'draft' WHERE status IS NULL;
UPDATE orders SET created_at = NOW() WHERE created_at IS NULL;
ALTER TABLE orders
    ALTER COLUMN origin_address SET DEFAULT '', ALTER COLUMN origin_address SET NOT NULL,
    ALTER COLUMN cargo_description SET DEFAULT '', ALTER COLUMN cargo_description SET NOT NULL,
    ALTER COLUMN weight_kg SET DEFAULT 0, ALTER COLUMN weight_kg SET NOT NULL,
    ALTER COLUMN volume_m3 SET DEFAULT 0, ALTER COLUMN volume_m3 SET NOT NULL,
    ALTER COLUMN status SET NOT NULL,
    ALTER COLUMN created_at SET NOT NULL;

UPDATE routes SET current_index = 0 WHERE current_index IS NULL;
UPDATE routes SET status = 'pending' WHERE status IS NULL;
ALTER TABLE routes
    ALTER COLUMN order_id SET NOT NULL,
    ALTER COLUMN current_index SET NOT NULL,
    ALTER COLUMN status SET NOT NULL;
-- +goose StatementEnd

-- +goose Down
-- +goose StatementBegin
ALTER TABLE routes
    ALTER COLUMN order_id DROP NOT NULL,
    ALTER COLUMN current_index DROP NOT NULL,
    ALTER COLUMN status DROP NOT NULL;
ALTER TABLE orders
    ALTER COLUMN origin_address DROP NOT NULL, ALTER COLUMN origin_address DROP DEFAULT,
    ALTER COLUMN cargo_description DROP NOT NULL, ALTER COLUMN cargo_description DROP DEFAULT,
    ALTER COLUMN weight_kg DROP NOT NULL, ALTER COLUMN weight_kg DROP DEFAULT,
    ALTER COLUMN volume_m3 DROP NOT NULL, ALTER COLUMN volume_m3 DROP DEFAULT,
    ALTER COLUMN status DROP NOT NULL,
    ALTER COLUMN created_at DROP NOT NULL;
ALTER TABLE managers ALTER COLUMN user_id DROP NOT NULL;
ALTER TABLE warehouses
    ALTER COLUMN city DROP NOT NULL, ALTER COLUMN city DROP DEFAULT,
    ALTER COLUMN status DROP NOT NULL, ALTER COLUMN status SET DEFAULT 'active',
    ALTER COLUMN created_at DROP NOT NULL;
ALTER TABLE drivers
    ALTER COLUMN user_id DROP NOT NULL,
    ALTER COLUMN rating DROP NOT NULL,
    ALTER COLUMN status DROP NOT NULL;
ALTER TABLE vehicles
    DROP CONSTRAINT vehicles_capacity_nonnegative,
    ALTER COLUMN capacity_kg DROP NOT NULL, ALTER COLUMN capacity_kg DROP DEFAULT,
    ALTER COLUMN capacity_m3 DROP NOT NULL, ALTER COLUMN capacity_m3 DROP DEFAULT,
    ALTER COLUMN status DROP NOT NULL, ALTER COLUMN status SET DEFAULT 'available';
ALTER TABLE users
    ALTER COLUMN full_name DROP NOT NULL, ALTER COLUMN full_name DROP DEFAULT,
    ALTER COLUMN avatar_url DROP NOT NULL, ALTER COLUMN avatar_url DROP DEFAULT,
    ALTER COLUMN role DROP NOT NULL, ALTER COLUMN role DROP DEFAULT,
    ALTER COLUMN created_at DROP NOT NULL,
    ALTER COLUMN updated_at DROP NOT NULL, ALTER COLUMN updated_at DROP DEFAULT;
-- +goose StatementEnd
