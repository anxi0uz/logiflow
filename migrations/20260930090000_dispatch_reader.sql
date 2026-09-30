-- +goose Up
-- +goose StatementBegin
DO $$
BEGIN
    CREATE ROLE dispatch_reader NOLOGIN;
EXCEPTION WHEN duplicate_object THEN
    NULL;
END
$$;

GRANT USAGE ON SCHEMA public TO dispatch_reader;
GRANT SELECT ON TABLE
    orders,
    drivers,
    driver_documents,
    driver_shifts,
    assignments,
    vehicles,
    vehicle_documents
TO dispatch_reader;
-- +goose StatementEnd

-- +goose Down
-- +goose StatementBegin
REVOKE SELECT ON TABLE
    orders,
    drivers,
    driver_documents,
    driver_shifts,
    assignments,
    vehicles,
    vehicle_documents
FROM dispatch_reader;
REVOKE USAGE ON SCHEMA public FROM dispatch_reader;
-- Keep the login: it can be shared by another database on this cluster.
-- +goose StatementEnd
