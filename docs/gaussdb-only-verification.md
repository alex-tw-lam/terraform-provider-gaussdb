# GaussDB-only verification — every resource, every option

The complete post-dialect verification pass: all 16 resources and 3 data
sources of this provider were applied to a live openGauss 5.0.0 container
with real Terraform runs, option by option, and every intended effect was
checked in the system catalogs (`pg_authid`, `pg_roles`, `pg_database`,
`pg_namespace`, `pg_auth_members`, `pg_default_acl`, `pg_tablespace`,
`pg_proc`, `pg_publication`, `pg_foreign_server`, `pg_replication_slots`,
`has_*_privilege()`, `information_schema.column_privileges`). Follows up the
baseline [compatibility matrix](compatibility-opengauss-5.0.md); all fixes
landed on the `gaussdb-driver` branch.

## Verified working (✅ = effect confirmed in catalog)

| Resource | ✅ Options verified | ❌ Rejected (cleanly) | Untested / niche |
| --- | --- | --- | --- |
| `postgresql_database` | name, owner, template, encoding, lc_collate, lc_ctype, tablespace_name, connection_limit (create+update), alter_object_ownership, **db_compatibility** (PG/A/B/C; Computed — openGauss default A no longer forces replacement), **revoke_public_connect** (on/off + read via aclexplode), **enable_private_object** (executed in the target db), destroy (terminate + DROP) | allow_connections, is_template (flavor-suppressed; explicit error when relied upon) | — |
| `postgresql_role` | name, login, password, encrypted_password, connection_limit, valid_until (tz-insensitive diff), statement_timeout, idle_in_transaction_session_timeout, search_path, create_database, create_role, inherit, replication, superuser→**SYSADMIN**, roles (membership), NOLOGIN + auto-generated password (openGauss mandates one), skip_drop_role (destroy leaves the role) | assume_role (server: `"SET role TO rolename" not yet supported`), bypass_row_level_security (clear provider error at create **and** update) | password_wo/password_wo_version, skip_reassign_owned |
| `postgresql_schema` | name, owner, if_not_exists, policy (usage/create) | — | rename path |
| `postgresql_extension` | hstore, postgres_fdw (create) | — | DROP EXTENSION fails server-side for some extensions (hstore) — destroy caveat |
| `postgresql_grant` | object_type database/schema/table/sequence/function/**column**, with_grant_option, negative case (ungranted privilege absent) | — | foreign_data_wrapper/foreign_server object types; routine/procedure (gated off at mapped PG 10) |
| `postgresql_grant_role` | role/grant_role direction, with_admin_option (admin_option=true in catalog) | — | do not overlap with `postgresql_role.roles` (both manage membership) |
| `postgresql_default_privileges` | table + sequence object types (pg_default_acl rows verified) | — | with_grant_option variant. NB: owner needs CREATE on the target schema — standard PostgreSQL semantics, not an openGauss quirk |
| `postgresql_function` | args, body, language (sql/plpgsql), returns, volatility, strict, **security_definer** (prosecdef=true), drop_cascade | **parallel** (clear provider error; openGauss has no PARALLEL clause / proparallel) | — |
| `postgresql_server` | server_name, fdw_name, server_type, server_version, options (map), server_owner (create **and update**), destroy | — | — |
| `postgresql_user_mapping` | server_name, user_name, options | — | — |
| `postgresql_publication` | tables, **all_tables**, **publish_param** (pubinsert/pubupdate verified), owner | — | publish_via_partition_root (gated off at mapped PG 10) |
| `postgresql_replication_slot` | logical slot with pgoutput (create/converge/destroy) | — | replica-server management |
| `postgresql_physical_replication_slot` | create/destroy (passes openGauss's dummy_standby=false) | — | — |
| `postgresql_tablespace` | name, owner, location (both flavors), **relative_location**, **maxsize** (create; update = ALTER TABLESPACE RESIZE MAXSIZE verified 1G→2G), quota diff-insensitive to unit normalization (1G == 1048576 K) | relative_location/maxsize on vanilla PostgreSQL (clear error) | — |
| `postgresql_subscription` | — | ❌ server-side: `WITH (create_slot/conninfo)` unknown + requires `subscription.key.cipher` keyfile infrastructure | — |
| `postgresql_security_label` | — | ❌ rejected at the feature gate with a clear message; server also answers "not yet supported" | — |
| data `postgresql_schemas` / `tables` / `sequences` | like patterns, schema filters, correct results | — | — |

## Fixes landed during this sweep

1. `bypass_row_level_security` now fails at **create** time on openGauss
   (previously silently omitted, then drifted).
2. `db_compatibility` is `Computed`: a database created without it reads
   back openGauss's default `A` without forcing replacement.
3. `postgresql_function.parallel` fails with a clear provider error on
   openGauss instead of a raw syntax error.

## Operational notes (openGauss)

- The initial user is SYSADMIN, not SUPERUSER: it does not bypass schema
  privilege checks, so `postgresql_default_privileges` for another owner
  needs that owner to hold `USAGE, CREATE` on the target schema (or run the
  provider with `superuser = false` for the membership dance).
- `postgresql_role.roles` and `postgresql_grant_role` manage the same
  membership — use one per pair, or they fight (perpetual diff).
- Reference stacks used for verification: an OSB-broker-shaped tenant
  (group role → login user → tablespace with quota → database with
  compatibility/isolation → membership) and an object stack (slots, FDW
  server + user mapping, function, publication) — both apply, converge
  (`terraform plan` exit 0), update and destroy cleanly.
