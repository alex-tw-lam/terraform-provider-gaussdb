# GaussDB / openGauss support

## Done: driver swap for sha256 authentication (branch `gaussdb-driver`)

lib/pq cannot authenticate against openGauss/GaussDB servers configured with
`password_encryption_type = 2` (sha256). The provider now connects through
[gaussdb-go](https://github.com/HuaweiCloudDeveloper/gaussdb-go) (the pgx v5
fork maintained by the same Huawei org as the Python driver), behind the same
proxy-capable dialer. See the commit message for the full change list.

Verified against a live openGauss 5.0.0 container with sha256 enabled:

```
GAUSSDB_SMOKE_DSN='postgres://gaussdb:…@localhost:25432/postgres?sslmode=disable' \
  go test ./postgresql/ -run TestGaussDBConnectionSmoke -v
```

## Known behavior differences

- DSNs are rewritten from `postgres://` to `gaussdb://` internally — the
  gaussdb parser only accepts `gaussdb://` URLs.
- `fallback_application_name` is sent as `application_name` (the gaussdb
  driver forwards unknown DSN parameters to the server, which rejects them).
- `ssl_inline` is rejected at connect time; the gaussdb driver only accepts
  `sslcert`/`sslkey` file paths.

## Version fingerprint caveat

openGauss reports `SELECT version()` as `(openGauss 5.0.0 build …) …`, which
the provider fingerprints as PostgreSQL 5.0.0. Every feature gate at
`>= 9.0.0` (including `postgresql_grant` and `postgresql_default_privileges`)
is then disabled. Until an openGauss version map lands (Phase 2.1 below),
operators must set the provider attribute:

```hcl
expected_version = "10.0.0"   # openGauss 5.x ≈ PostgreSQL 10 compatibility
```

## Design proposal: parity with the osb-opengauss-go broker

The broker ([osb-opengauss-go](https://github.com/alex-tw-lam/osb-opengauss-go))
provisions tenants with the SQL surface below. Mapping onto this provider:

| Broker SQL | Provider resource | Status |
| --- | --- | --- |
| `CREATE USER … LOGIN PASSWORD`, `ALTER ROLE … PASSWORD`, `DROP USER` | `postgresql_role` | exists |
| `GRANT role TO member` / `REVOKE` | `postgresql_grant_role` | exists |
| `GRANT/REVOKE CONNECT, USAGE, CREATE ON DATABASE/SCHEMA` | `postgresql_grant` | exists (needs version map) |
| `ALTER DEFAULT PRIVILEGES …` | `postgresql_default_privileges` | exists |
| `CREATE DATABASE OWNER … TEMPLATE … ENCODING … TABLESPACE … CONNECTION LIMIT` | `postgresql_database` | exists |
| `ALTER DATABASE … CONNECTION LIMIT` (update) | `postgresql_database` | exists |
| `DROP DATABASE` + `pg_terminate_backend` | `postgresql_database` delete | exists (PG13 `WITH FORCE` path must stay gated off for openGauss) |
| terminate sessions + `DROP OWNED BY … CASCADE` on unbind | `postgresql_role` delete | exists (`DROP OWNED BY`); add session termination |
| `CREATE DATABASE … DBCOMPATIBILITY {PG,A,B,C}` | — | **gap**: add `db_compatibility` arg (ForceNew) |
| `REVOKE CONNECT ON DATABASE … FROM PUBLIC` | — | **gap**: add `revoke_public_connect` bool on `postgresql_database` |
| `ALTER DATABASE … ENABLE PRIVATE OBJECT` | — | **gap**: add `enable_private_object` bool (openGauss-only) |
| `CREATE TABLESPACE … OWNER … RELATIVE LOCATION … MAXSIZE …` | — | **gap**: new resource `postgresql_tablespace` |
| `ALTER TABLESPACE … RESIZE MAXSIZE …` (update) | — | **gap**: same new resource |

### Phase 2 — close the gaps (est. 4–6 engineer-days, live container available for verification)

1. **Version map** (0.5d): fingerprint recognizes `openGauss X.Y` / `GaussDB`
   in `SELECT version()` and evaluates feature gates against the PostgreSQL
   compatibility level (openGauss 5.x ≈ PG 10) instead of 5.0.0.
2. **`postgresql_tablespace` resource** (2–3d): create with `owner`,
   `relative_location`, `maxsize`; update via `ALTER TABLESPACE … RESIZE
   MAXSIZE`; delete. openGauss-only clauses are gated on the detected flavor.
3. **`postgresql_database` additions** (1–2d): `db_compatibility`
   (ForceNew, enum PG/A/B/C), `revoke_public_connect`,
   `enable_private_object`.
4. **Role-delete parity** (0.5d): terminate the role's backends before
   `DROP OWNED BY … CASCADE`, matching broker unbind semantics.

### Phase 3 — optional (2–3d)

Acceptance tests (`TF_ACC`) against an `opengauss/opengauss` container, plus
an example module that mirrors the broker's full provision → bind → unbind →
deprovision flow.

### Upstream note

This fork is GaussDB-only (lib/pq is no longer a direct dependency). Becoming
dual-backend/PG-compatible for an upstream PR to cyrilgdn is a separate,
larger restructuring and is deliberately not attempted here.
