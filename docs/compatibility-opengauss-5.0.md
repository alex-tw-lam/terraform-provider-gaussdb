# openGauss 5.0 compatibility matrix

> **Status**: this matrix documents the *baseline* (pre-dialect) state.
> Every fixable gap below — role dialect, database clauses/params, slots,
> function/server read paths, plus the new `postgresql_tablespace`
> resource — has since been implemented and live-verified; see "Done:
> GaussDB dialect (Phase 2)" in [GAUSSDB.md](../GAUSSDB.md). Only
> `postgresql_subscription` and `postgresql_security_label` remain
> unsupported (server-side).

Every provider resource (and data source) was exercised against a live
openGauss 5.0.0 container (`opengauss/opengauss:5.0.0`, `password_encryption_type=2`,
`wal_level=logical`) using real Terraform 1.17.1 runs through dev_overrides,
then each option's intended effect was verified in the catalogs
(`pg_authid`, `pg_database`, `pg_namespace`, `pg_auth_members`,
`pg_default_acl`, `pg_foreign_server`, `pg_publication`, `pg_replication_slots`,
`has_*_privilege()`).

Provider config used for the runs: `username=gaussdb` (initial user, SYSADMIN),
`database=postgres`, `sslmode=disable`, and `expected_version` as noted below.

## Summary

| Resource | Verdict | Blocker |
| --- | --- | --- |
| `postgresql_database` | ✅ works¹ | ¹ with `expected_version ≤ 9.4` |
| `postgresql_role` | ❌ fails | `SUPERUSER`/`NOSUPERUSER`/`BYPASSRLS` unknown; password mandatory |
| `postgresql_schema` | ✅ works | — |
| `postgresql_extension` | ✅ works | (`DROP EXTENSION` unsupported server-side; destroy will fail) |
| `postgresql_grant` | ✅ works | — |
| `postgresql_grant_role` | ✅ works | — |
| `postgresql_default_privileges` | ✅ works² | ² needs schema `USAGE` for the admin user |
| `postgresql_function` | ⚠️ partial | CREATE works; READ fails — `to_regprocedure()` missing |
| `postgresql_server` | ⚠️ partial | CREATE works; READ fails — `::regrole` cast missing |
| `postgresql_user_mapping` | ✅ works | — |
| `postgresql_publication` | ✅ works | — |
| `postgresql_replication_slot` (logical) | ❌ fails³ | ³ provider SQL `SELECT FROM func()`; fixable |
| `postgresql_physical_replication_slot` | ❌ fails³ | ³ same one-line SQL issue |
| `postgresql_subscription` | ❌ fails | `WITH (create_slot/conninfo)` unknown + requires `subscription.key.cipher` infrastructure |
| `postgresql_security_label` | ❌ fails | server: `SECURITY LABEL is not yet supported` (0A000) |
| data `postgresql_schemas` | ✅ works | — |
| data `postgresql_sequences` | ✅ works | — |
| data `postgresql_tables` | ✅ works | — |

**Score: 9 fully working, 2 partial (read-path SQL), 5 failing (2 trivially
fixable, 3 server-side).** The four resources the OSB broker needs
(database, role, schema/grant/grant_role/default_privileges family) are —
after the role dialect fix — all in the working set.

## Detail per resource

### postgresql_database — ✅ (expected_version ≤ 9.4)

Verified end-to-end: `name`, `owner`, `connection_limit` (create + update),
`tablespace_name`, `template`, `encoding`, `lc_collate`, `lc_ctype` — all
confirmed in `pg_database`. Update path (`ALTER DATABASE … CONNECTION LIMIT`)
and destroy (terminate + `DROP DATABASE`) both work.

- Provider emits `ALLOW_CONNECTIONS` / `IS_TEMPLATE` once
  `expected_version ≥ 9.5`; openGauss rejects both clauses (syntax error).
  Setting `expected_version = "9.4.0"` suppresses them and everything passes.
- **`DBCOMPATIBILITY` works** (SQL-probed) but is not exposed by the provider —
  and openGauss defaults new databases to compatibility **`A` (Oracle)**, so
  this gap really matters for PG-expecting users.

### postgresql_role — ❌ (dialect fix required)

Provider always emits `SUPERUSER|NOSUPERUSER` (and `BYPASSRLS` when unset);
openGauss rejects them. SQL probe of openGauss's accepted options:

| Accepted | Rejected | openGauss-only extras |
| --- | --- | --- |
| `SYSADMIN`, `CREATEDB/NOCREATEDB`, `CREATEROLE/NOCREATEROLE`, `INHERIT/NOINHERIT`, `LOGIN/NOLOGIN`, `REPLICATION/NOREPLICATION`, `CONNECTION LIMIT`, `VALID UNTIL`, `PASSWORD`, `ENCRYPTED PASSWORD` | `SUPERUSER`, `NOSUPERUSER`, `BYPASSRLS` | `VALID BEGIN`, `AUDITADMIN`, `MONADMIN`, `OPRADMIN`, `POLADMIN`, `VCADMIN` |

Additional hardening: **every `CREATE ROLE` requires a `PASSWORD`** — even
`NOLOGIN` group roles (`The password could not be NULL`, 28P01). The broker
already handles this (it generates a group password); the provider must do
the same for openGauss. Fix shape: map `superuser`→`SYSADMIN`, drop
`BYPASSRLS`, auto-generate a password for `NOLOGIN` roles.

### postgresql_schema — ✅

`name`, `owner`, and `policy` (usage/create grants via `has_schema_privilege`)
all verified. No openGauss issues.

### postgresql_extension — ✅ (with destroy caveat)

`hstore` and `postgres_fdw` both created and visible in `pg_extension`.
Caveat: `DROP EXTENSION` reports `EXTENSION is not yet supported` on this
build, so terraform destroy of an extension fails server-side.

### postgresql_grant — ✅

All object types verified with intended (and unintended-privilege-absent)
effects: `database` (CONNECT/TEMPORARY), `schema` (USAGE/CREATE),
`table` (SELECT/INSERT/UPDATE granted; DELETE correctly absent),
`sequence` (USAGE/SELECT), `function` (EXECUTE).

### postgresql_grant_role — ✅

Membership lands in `pg_auth_members` (note the provider's argument
direction: with `role="tf_user"`, `grant_role="tf_grp"`, `tf_user` becomes a
member of `tf_grp` — matching broker bind semantics).

### postgresql_default_privileges — ✅ (needs admin schema USAGE)

`pg_default_acl` row created for `owner=tf_user` in `tf_schema` (table
defaults to the grantee). openGauss's initial user is **SYSADMIN, not
SUPERUSER**, and does not bypass schema USAGE checks: `ALTER DEFAULT
PRIVILEGES FOR ROLE … IN SCHEMA …` fails with `permission denied for schema`
until the admin has `USAGE` on it. Either grant the admin `USAGE` on managed
schemas, or run the provider with `superuser = false` so it does the
role-membership dance itself.

### postgresql_function — ⚠️ partial

The function is created correctly (`tf_fn_add(integer,integer)` in
`pg_proc`), but the resource never converges: its READ path uses
`to_regprocedure(...)`, which does not exist on openGauss 5.0
(`42883`). Fix: resolve the function via `pg_proc`/`pg_namespace` lookups
instead of `to_regprocedure`.

### postgresql_server / postgresql_user_mapping — ⚠️ / ✅

`CREATE SERVER … OPTIONS (host, dbname, port)` lands correctly in
`pg_foreign_server` (options verified), but READ fails on the `fs.srvowner::regrole`
cast (`regrole` type does not exist on openGauss; `regnamespace` also
missing). `postgresql_user_mapping` itself works (`umoptions` row verified)
once the server exists — the failure seen in the combined run was only the
cascade from the server read error.

### postgresql_publication — ✅

`CREATE PUBLICATION … FOR TABLE` works; publication and table list verified
in `pg_publication`.

### Replication slots — ❌ (one-line provider fix)

Both slot resources issue `SELECT FROM pg_create_(logical|physical)_replication_slot(...)`
— a bare `SELECT FROM func()` with no target list that openGauss's parser
rejects (`syntax error at or near "FROM"`). The underlying functions exist
and work when called as `SELECT pg_create_logical_replication_slot('x','pgoutput')`
(verified: logical `pgoutput` slot created and dropped, `wal_level=logical`).

### postgresql_subscription — ❌ (server-side)

Two independent blockers, both confirmed by SQL probes:

1. openGauss's `CREATE SUBSCRIPTION … WITH` does not accept `create_slot` or
   `conninfo` as parameters (`unrecognized subscription parameter`).
2. Even with accepted parameters, creation fails with `No key file
   subscription.key.cipher` — openGauss subscriptions depend on GaussDB
   keyfile infrastructure that a plain container/this deployment lacks.
   Enterprise GaussDB logical replication has its own mechanism anyway.

### postgresql_security_label — ❌ (server-side)

At `expected_version = 10` the provider's feature gate refuses it; raised to
16 the server answers `SECURITY LABEL is not yet supported` (0A000). Not
implementable on openGauss 5.0 in any form.

### Data sources — ✅

`postgresql_schemas` (`like_any_patterns`), `postgresql_sequences`
(`schemas`), `postgresql_tables`: all return correct results.

## Cross-cutting findings

- **Version fingerprint**: `SELECT version()` returns `(openGauss 5.0.0 …)`,
  fingerprinted as 5.0.0, which disables everything gated `>= 9` (grants
  included). The Phase-2 version map must both map openGauss to ~PG9.4–10
  semantics and (better) suppress `ALLOW_CONNECTIONS`/`IS_TEMPLATE` per
  flavor so a single `expected_version` isn't needed per resource.
- **SYSADMIN ≠ SUPERUSER**: openGauss's initial user does not bypass
  privilege checks the way a PG superuser does (schema USAGE above; the
  provider's `superuser = true` fast paths can be too optimistic).
- **Missing PG niceties on this build**: `to_regprocedure()`, `regrole`,
  `regnamespace`, `DROP EXTENSION`.
- **Default `DBCOMPATIBILITY` is `A` (Oracle)** for new databases unless set
  explicitly — worth surfacing in the provider's database resource.

## Revised Phase-2 scope (GaussDB-only direction)

1. Role dialect (SYSADMIN mapping, mandatory password, drop BYPASSRLS) — 1–2 d
2. openGauss version map + flavor-gated database clauses — 1 d
3. `postgresql_tablespace` resource (RELATIVE LOCATION/MAXSIZE/RESIZE) — 2–3 d
4. Database additions (`db_compatibility`, `revoke_public_connect`,
   `enable_private_object`) — 1–2 d
5. Slot SQL one-liner (`SELECT func(...)`) — 0.25 d
6. function/server read-path rewrites (`to_regprocedure`/`regrole`) — 1 d

~6–9 engineer-days total. `postgresql_subscription` and
`postgresql_security_label` stay unsupported (server-side); document them as
such.
