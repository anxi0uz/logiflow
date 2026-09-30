SELECT 'CREATE ROLE dispatch_reader LOGIN'
WHERE NOT EXISTS (SELECT 1 FROM pg_roles WHERE rolname = 'dispatch_reader')
\gexec

ALTER ROLE dispatch_reader LOGIN PASSWORD :'dispatch_password';
SELECT format('GRANT CONNECT ON DATABASE %I TO dispatch_reader', current_database())
\gexec
