# terraform-provider-gaussdb

A Terraform provider for **openGauss / GaussDB** (including Huawei Cloud HCS
enterprise GaussDB), managing the logical-database layer: tenants, roles,
grants, tablespaces with storage quotas, publications, replication slots and
more — with openGauss's native **sha256 authentication**.

GaussDB-only fork of
[cyrilgdn/terraform-provider-postgresql](https://github.com/cyrilgdn/terraform-provider-postgresql):
the lib/pq driver is replaced by Huawei's
[gaussdb-go](https://github.com/HuaweiCloudDeveloper/gaussdb-go) and every
resource speaks the openGauss dialect (`SYSADMIN` roles, `DBCOMPATIBILITY`,
`ENABLE PRIVATE OBJECT`, `RELATIVE LOCATION`/`MAXSIZE` tablespaces, …).
Vanilla PostgreSQL is not supported. Every resource and option is
live-verified against openGauss 5.0 — see
[GAUSSDB.md](GAUSSDB.md) and
[docs/gaussdb-only-verification.md](docs/gaussdb-only-verification.md).

## Resources

`gaussdb_database` · `gaussdb_role` · `gaussdb_schema` · `gaussdb_grant` ·
`gaussdb_grant_role` · `gaussdb_default_privileges` · `gaussdb_tablespace` ·
`gaussdb_extension` · `gaussdb_function` · `gaussdb_server` ·
`gaussdb_user_mapping` · `gaussdb_publication` · `gaussdb_replication_slot` ·
`gaussdb_physical_replication_slot`

Not supported (server-side): `gaussdb_subscription`, `gaussdb_security_label`.

## Install

Build from source and use a dev override:

```sh
go build -o terraform-provider-gaussdb .
```

```hcl
terraform {
  required_providers {
    gaussdb = { source = "alex-tw-lam/gaussdb" }
  }
}
```

```hcl
provider "gaussdb" {
  host     = "10.0.0.99"
  port     = 8000
  username = "root"
  password = var.db_password
  database = "postgres"
  sslmode  = "disable"
}
```

## Example: one tenant, broker-style

The broker's tenant blueprint ships as a module at
[modules/tenant](modules/tenant) — NOLOGIN group role, tablespace with a
resizable quota, database with compatibility and isolation options, the
broker-parity grants inside it, and an application login. Configure the
provider in your root module, then:

```hcl
module "orders" {
  source = "github.com/alex-tw-lam/terraform-provider-gaussdb//modules/tenant"

  tenant       = "orders"
  app_password = var.orders_app_password
  # storage_quota, connection_limit, db_compatibility,
  # revoke_public_connect, enable_private_object all have defaults
}

output "tenant_database" { value = module.orders.database }
```

Outputs: `database`, `group_role`, `app_role`, `tablespace`. The module is
live-verified end-to-end against openGauss 5.0.0 (apply, converge, grants
confirmed via has_*_privilege, destroy).

## OpenTofu

Works with OpenTofu as-is (verified with 1.13). Two differences: OpenTofu
resolves `alex-tw-lam/gaussdb` to **registry.opentofu.org**, so mirror
layouts and `include` filters need that hostname alongside
`registry.terraform.io`; and the CLI config lives in `~/.tofurc` (the
`TF_CLI_CONFIG_FILE` env var is not honoured).

## License

MPL-2.0, as upstream.
