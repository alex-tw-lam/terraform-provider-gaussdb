# GaussDB / openGauss support

## Done: driver swap for sha256 authentication (branch `gaussdb-driver`)

lib/pq cannot authenticate against openGauss/GaussDB servers configured with
`password_encryption_type = 2` (sha256). The provider now connects through
[gaussdb-go](https://github.com/HuaweiCloudDeveloper/gaussdb-go) (the pgx v5
fork maintained by the same Huawei org as the Python driver), behind the same
proxy-capable dialer. See the commit message for the full change list.

## GaussDB-only

This provider targets openGauss/GaussDB exclusively. It connects through
the upstream [gaussdb-go](https://github.com/HuaweiCloudDeveloper/gaussdb-go)
driver (pgx v5 fork, maintained by Huawei), which speaks openGauss's native
sha256 authentication (`password_encryption_type = 2`) as well as md5
(type 0/1). Vanilla PostgreSQL servers are **not** supported: the upstream
driver requests GaussDB's protocol version (3, 51) and cannot complete the
negotiation or SCRAM exchange that standard PostgreSQL requires. (A fork
with those two protocol fixes exists at
[alex-tw-lam/database-gaussdb-go](https://github.com/alex-tw-lam/database-gaussdb-go)
but this provider deliberately does not depend on it.)

Verified against a live openGauss 5.0.0 container with sha256 enabled via
the opt-in smoke test:

```
GAUSSDB_SMOKE_DSN='postgres://gaussdb:…@localhost:25432/postgres?sslmode=disable' \
  go test ./gaussdb/ -run TestGaussDBConnectionSmoke -v
```

## Known behavior differences

- DSNs are rewritten from `postgres://` to `gaussdb://` internally — the
  gaussdb parser only accepts `gaussdb://` URLs.
- `fallback_application_name` is sent as `application_name` (the gaussdb
  driver forwards unknown DSN parameters to the server, which rejects them).
- `ssl_inline` is rejected at connect time; the gaussdb driver only accepts
  `sslcert`/`sslkey` file paths.

## Done: GaussDB dialect (Phase 2)

Full per-resource, per-option verification lives in
[docs/gaussdb-only-verification.md](docs/gaussdb-only-verification.md).

All the gaps found by the [compatibility matrix](docs/compatibility-opengauss-5.0.md)
are closed, live-verified against openGauss 5.0.0 with full Terraform
lifecycles (create → converged plan → update → destroy):

- **Flavor detection + version map**: `SELECT version()` is fingerprinted on
  every connect; openGauss servers are mapped to their PostgreSQL
  compatibility level (openGauss ≥5 → 10.0.0), so `expected_version` is no
  longer needed per resource. `ALLOW_CONNECTIONS`/`IS_TEMPLATE` clauses are
  suppressed per flavor.
- **Role dialect**: `superuser` maps to `SYSADMIN` (and reads back from
  `rolsystemadmin`), `BYPASSRLS` is rejected with a clear error, and a random
  password is generated for roles created without one — openGauss mandates a
  password on every `CREATE ROLE`, even `NOLOGIN` group roles. sha256-stored
  passwords no longer leak their server hash into state.
- **New `gaussdb_tablespace` resource**: `owner`, `location` (portable),
  `relative_location` + `maxsize` (openGauss; update resizes via
  `ALTER TABLESPACE … RESIZE MAXSIZE`). Quota strings compare by byte value
  (`1G` == `1048576 K`).
- **`gaussdb_database` additions**: `db_compatibility` (PG/A/B/C; PG-only
  servers accept just "PG"; openGauss defaults new databases to A!),
  `revoke_public_connect` (updates with GRANT/REVOKE … TO/FROM PUBLIC),
  `enable_private_object` (openGauss only, executed inside the target
  database; write-only — the server exposes no readable flag).
- **Replication slots**: `SELECT func(...)` instead of PostgreSQL-only
  `SELECT FROM func()`; the physical variant passes openGauss's
  `dummy_standby` argument.
- **Read paths**: function existence/definition resolve via the
  `::regprocedure` cast (with 42883 = not exists) instead of the missing
  `to_regprocedure()`; the foreign server owner reads via `pg_get_userbyid`
  instead of the missing `::regrole` cast.
- `valid_until` compares timestamps as instants (the server may answer in
  its own timezone).

### Unsupported on openGauss/GaussDB (server-side)

- `gaussdb_subscription` — WITH parameters differ and creation requires
  GaussDB keyfile infrastructure (`subscription.key.cipher`).
- `gaussdb_security_label` — server answers "SECURITY LABEL is not yet
  supported" (0A000).
- `DROP EXTENSION` fails for some extensions on openGauss 5.0 builds
  ("EXTENSION is not yet supported") — destroying such an extension resource
  will error server-side.

## Broker parity status

Every SQL statement the osb-opengauss-go broker issues now has a provider
counterpart, live-verified end-to-end (a Terraform stack shaped exactly like
the broker's tenant provisioning — group role, login user, tablespace with
quota, database with compatibility/isolation, role membership — applies,
converges, updates and destroys cleanly):

| Broker SQL | Provider resource | Status |
| --- | --- | --- |
| `CREATE ROLE … NOLOGIN PASSWORD` (group role) | `gaussdb_role` | ✅ (password auto-generated when unset) |
| `CREATE USER … LOGIN PASSWORD`, `ALTER ROLE …` | `gaussdb_role` | ✅ |
| `GRANT role TO member` / `REVOKE` | `gaussdb_grant_role` | ✅ |
| `GRANT/REVOKE CONNECT, USAGE, CREATE …` | `gaussdb_grant` | ✅ |
| `ALTER DEFAULT PRIVILEGES …` | `gaussdb_default_privileges` | ✅ |
| `CREATE DATABASE OWNER … TEMPLATE … ENCODING … TABLESPACE … CONNECTION LIMIT` | `gaussdb_database` | ✅ |
| `CREATE DATABASE … DBCOMPATIBILITY {PG,A,B,C}` | `gaussdb_database` `db_compatibility` | ✅ |
| `REVOKE CONNECT ON DATABASE … FROM PUBLIC` | `gaussdb_database` `revoke_public_connect` | ✅ |
| `ALTER DATABASE … ENABLE PRIVATE OBJECT` | `gaussdb_database` `enable_private_object` | ✅ (write-only flag) |
| `CREATE TABLESPACE … OWNER … RELATIVE LOCATION … MAXSIZE …` | `gaussdb_tablespace` | ✅ |
| `ALTER TABLESPACE … RESIZE MAXSIZE …` | `gaussdb_tablespace` `maxsize` update | ✅ |
| `ALTER DATABASE … CONNECTION LIMIT` | `gaussdb_database` update | ✅ |
| `DROP DATABASE` + `pg_terminate_backend` | `gaussdb_database` delete | ✅ |
| terminate + `DROP OWNED BY … CASCADE` on unbind | `gaussdb_role` delete | ✅ |

### Phase 3 — optional follow-ups

- Acceptance tests (`TF_ACC`) against an `opengauss/opengauss` container in CI.
- An example module mirroring the broker's full provision → bind → unbind →
  deprovision flow.

### Upstream note

This is a GaussDB-only fork of
[cyrilgdn/terraform-provider-postgresql](https://github.com/cyrilgdn/terraform-provider-postgresql)
(lib/pq is no longer a direct dependency; the driver, DSN plumbing and SQL
dialect all target openGauss/GaussDB). The protocol fixes that would let the
driver also reach vanilla PostgreSQL live unused in
[alex-tw-lam/database-gaussdb-go](https://github.com/alex-tw-lam/database-gaussdb-go)
and would make a good upstream PR to Huawei.
