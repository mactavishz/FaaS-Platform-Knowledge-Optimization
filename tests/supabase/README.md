# Supabase test database scripts

This directory contains a helper script to create, truncate, drop, and inspect the test database tables used by the test function workflows.

The admin script requires `SUPABASE_DB_HOST` and `SUPABASE_DB_PASSWORD` and uses `psql` or a Docker fallback.

Optional environment variables:

- `SUPABASE_DB_USER` (defaults to `postgres`)
- `SUPABASE_DB_PORT` (defaults to `5432`)
- `SUPABASE_DB_NAME` (defaults to `postgres`)

## Script usage

From the repository root:

```bash
export SUPABASE_DB_HOST="<your-session-pooler-host>"
export SUPABASE_DB_USER="postgres.<project-ref>"
export SUPABASE_DB_PORT="5432"
export SUPABASE_DB_PASSWORD="<your-supabase-db-password>"
```

For IPv4-only networks, use the Supabase Session pooler endpoint from the project's Connect page.
The pooler host is typically `aws-<region>.pooler.supabase.com`, and the matching user is usually
`postgres.<project-ref>`.

If you have IPv6 connectivity, or your project has the Supabase IPv4 add-on, you can use the direct
database host (`db.<project-ref>.supabase.co`) instead. In that case, the default `postgres` user is
usually sufficient.

## IoT workflow tables (`sensor_data`, `use_case`)

- Create tables: `bash tests/supabase/scripts/db.sh create iot`
- Truncate tables: `bash tests/supabase/scripts/db.sh reset iot`
- Drop tables: `bash tests/supabase/scripts/db.sh destroy iot`
- Get all rows: `bash tests/supabase/scripts/db.sh get iot`

## Webshop workflow tables (`webshop_cart`)

- Create tables: `bash tests/supabase/scripts/db.sh create webshop`
- Truncate tables: `bash tests/supabase/scripts/db.sh reset webshop`
- Drop tables: `bash tests/supabase/scripts/db.sh destroy webshop`
- Get all rows: `bash tests/supabase/scripts/db.sh get webshop`

## Caveats

- `SUPABASE_DB_HOST` is passed to `psql` via `-h`, so it must be a host/IP (not a `postgres://...` DSN).
- The direct `db.<project-ref>.supabase.co` host can be IPv6-only. If it does not resolve or connect from your machine, switch to the Session pooler host instead.
- The script falls back to `docker run postgres:18-alpine ... psql ...` if local `psql` is missing. The Docker path passes `PGPASSWORD`, but any extra PostgreSQL connection env vars such as SSL settings still need to be handled separately.
