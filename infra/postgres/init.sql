-- Local development credentials only. Production secrets are generated separately.
CREATE ROLE minoduck_app LOGIN PASSWORD 'local-only-password' NOSUPERUSER NOBYPASSRLS NOCREATEDB NOCREATEROLE;
GRANT CONNECT ON DATABASE minoduck TO minoduck_app;
GRANT USAGE ON SCHEMA public TO minoduck_app;
ALTER DEFAULT PRIVILEGES FOR ROLE minoduck_migrator IN SCHEMA public GRANT SELECT,INSERT,UPDATE,DELETE ON TABLES TO minoduck_app;
ALTER DEFAULT PRIVILEGES FOR ROLE minoduck_migrator IN SCHEMA public GRANT USAGE,SELECT ON SEQUENCES TO minoduck_app;
