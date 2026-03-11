# Supabase test database scripts

This directory contains helper scripts to create, truncate, and drop the test database tables used by the test function workflows.

The admin scripts require `SUPABASE_DB_URL` and use `psql` or a Docker fallback.

Running these scripts will ask you to provide the password for the database.

## IoT workflow tables (`sensor_data`, `use_case`)

- Create tables: `tests/supabase/scripts/db_iot_up.sh`
- Truncate tables: `tests/supabase/scripts/db_iot_truncate.sh`
- Drop tables: `tests/supabase/scripts/db_iot_down.sh`

## Webshop workflow tables (`webshop_cart`)

- Create tables: `tests/supabase/scripts/db_webshop_up.sh`
- Truncate tables: `tests/supabase/scripts/db_webshop_truncate.sh`
- Drop tables: `tests/supabase/scripts/db_webshop_down.sh`

## Caveats

- `SUPABASE_DB_URL` is passed to `psql` via `-h`, so it must be a host/IP (not a `postgres://...` DSN).
- The scripts fall back to `docker run postgres:18-alpine ... psql ...` if local `psql` is missing, but that docker path does not pass auth/SSL env vars into the container.
