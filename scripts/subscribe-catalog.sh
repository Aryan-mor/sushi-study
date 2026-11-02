#!/usr/bin/env bash
# Point one environment's database at the library's catalog (ADR 0005).
#
# This is the whole of "set up a new environment": create the database, run the env migrations,
# run this. No files are copied and no fetcher runs — the catalog arrives by replication and stays
# current on its own (R-LIB-5).
#
# The connection string lives in the environment, never in a migration (R-SEC-1).
set -euo pipefail

: "${DATABASE_URL:?the environment database to subscribe}"
: "${LIBRARY_URL:?the library database to subscribe to}"

SUB_NAME="${SUB_NAME:-sushi_catalog}"

echo "subscribing $(psql "$DATABASE_URL" -tAc 'select current_database()') to the library"

# The slot is created first, on the publisher, so the subscriber never has to wait for it.
psql "$LIBRARY_URL" -tAc   "select case when exists (select 1 from pg_replication_slots where slot_name = '${SUB_NAME}')
          then 'slot ${SUB_NAME} already exists'
          else (select 'created ' || slot_name from pg_create_logical_replication_slot('${SUB_NAME}', 'pgoutput')) end"

psql "$DATABASE_URL" -v ON_ERROR_STOP=1 <<SQL
DO \$\$
BEGIN
  IF EXISTS (SELECT 1 FROM pg_subscription WHERE subname = '${SUB_NAME}') THEN
    RAISE NOTICE 'subscription ${SUB_NAME} already exists';
  ELSE
    -- create_slot=false plus a slot made beforehand, because CREATE SUBSCRIPTION otherwise waits
    -- for the publisher inside its own transaction. When library and environment sit on the same
    -- server -- which is exactly the dev setup -- that wait never returns.
    EXECUTE format(
      'CREATE SUBSCRIPTION ${SUB_NAME} CONNECTION %L PUBLICATION sushi_catalog '
      || 'WITH (create_slot = false, slot_name = ''${SUB_NAME}'', copy_data = true)',
      '${LIBRARY_URL}'
    );
  END IF;
END
\$\$;
SQL

# Pick up any table added to or dropped from the publication since this environment first
# subscribed. No-op on the common path; safe to run every time. It cannot live in a migration --
# ALTER SUBSCRIPTION ... REFRESH PUBLICATION refuses to run inside a transaction block, which is
# what goose wraps a migration in.
psql "$DATABASE_URL" -v ON_ERROR_STOP=1 -tAc "ALTER SUBSCRIPTION ${SUB_NAME} REFRESH PUBLICATION"

psql "$DATABASE_URL" -tAc "
  select 'subscription ' || subname || ' enabled=' || subenabled from pg_subscription;"
