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

```hcl
resource "gaussdb_role" "tenant_grp" {
  name = "orders_grp" # NOLOGIN; password auto-generated (openGauss requires one)
}

resource "gaussdb_tablespace" "tenant_ts" {
  name              = "orders_ts"
  owner             = gaussdb_role.tenant_grp.name
  relative_location = "orders_ts"
  maxsize           = "20G" # ALTER TABLESPACE … RESIZE MAXSIZE on update
}

resource "gaussdb_database" "tenant_db" {
  name                  = "orders"
  owner                 = gaussdb_role.tenant_grp.name
  tablespace_name       = gaussdb_tablespace.tenant_ts.name
  connection_limit      = 64
  db_compatibility      = "PG"   # openGauss defaults new databases to A (Oracle)
  revoke_public_connect = true
  enable_private_object = true
}

resource "gaussdb_role" "app" {
  name     = "orders_app"
  login    = true
  password = var.app_password
  roles    = [gaussdb_role.tenant_grp.name]
}
```

## License

MPL-2.0, as upstream.
