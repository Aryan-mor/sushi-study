-- One-time setup of the Postgres cluster: role sushi_app, databases sushi_library, sushi_dev and
-- sushi_prod, and the pg_trgm extension. Safe to re-run. See the README for the steps after this.

\set ON_ERROR_STOP on

DO $$
BEGIN
  IF NOT EXISTS (SELECT 1 FROM pg_roles WHERE rolname = 'sushi_app') THEN
    CREATE ROLE sushi_app LOGIN;
  END IF;
END
$$;

ALTER ROLE sushi_app RESET statement_timeout;
ALTER ROLE sushi_app RESET idle_in_transaction_session_timeout;
ALTER ROLE sushi_app RESET lock_timeout;

SELECT 'CREATE DATABASE sushi_library OWNER sushi_app'
 WHERE NOT EXISTS (SELECT FROM pg_database WHERE datname = 'sushi_library')\gexec

SELECT 'CREATE DATABASE sushi_dev OWNER sushi_app'
 WHERE NOT EXISTS (SELECT FROM pg_database WHERE datname = 'sushi_dev')\gexec

SELECT 'CREATE DATABASE sushi_prod OWNER sushi_app'
 WHERE NOT EXISTS (SELECT FROM pg_database WHERE datname = 'sushi_prod')\gexec

\connect sushi_library
CREATE EXTENSION IF NOT EXISTS pg_trgm;

\connect sushi_dev
CREATE EXTENSION IF NOT EXISTS pg_trgm;

\connect sushi_prod
CREATE EXTENSION IF NOT EXISTS pg_trgm;


