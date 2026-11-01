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

A complete, live-verified module (applied, converged and destroyed against
openGauss 5.0.0): variables, the full broker-parity grant set and outputs
included.

```hcl
terraform {
  required_providers {
    gaussdb = { source = "alex-tw-lam/gaussdb" }
  }
}

variable "db_host" {
  description = "GaussDB address (the cluster VIP on HCS)"
  type        = string
}

variable "db_port" {
  type    = number
  default = 8000
}

variable "db_admin_user" {
  description = "Sysadmin account the provider connects with"
  type        = string
  default     = "root"
}

variable "db_admin_password" {
  type      = string
  sensitive = true
}

variable "tenant" {
  description = "Tenant name; prefixes every generated object (max 55 chars)"
  type        = string
}

variable "app_password" {
  description = "Login password of the tenant application role"
  type        = string
  sensitive   = true
}

variable "storage_quota" {
  description = "Tablespace quota, e.g. \"20G\"; updates resize in place"
  type        = string
  default     = "20G"
}

provider "gaussdb" {
  host     = var.db_host
  port     = var.db_port
  username = var.db_admin_user
  password = var.db_admin_password
  database = "postgres"
  sslmode  = "disable"
}

# The tenant's group role: NOLOGIN owner of everything below. openGauss
# mandates a password on every role, so one is generated (and deliberately
# not stored in state — the value is irrelevant for a NOLOGIN role).
resource "gaussdb_role" "tenant_grp" {
  name = "${var.tenant}_grp"
}

# Storage quota; changing storage_quota resizes via ALTER TABLESPACE.
resource "gaussdb_tablespace" "tenant_ts" {
  name              = "${var.tenant}_ts"
  owner             = gaussdb_role.tenant_grp.name
  relative_location = var.tenant
  maxsize           = var.storage_quota
}

resource "gaussdb_database" "tenant_db" {
  name                  = var.tenant
  owner                 = gaussdb_role.tenant_grp.name
  tablespace_name       = gaussdb_tablespace.tenant_ts.name
  connection_limit      = 64
  db_compatibility      = "PG"   # openGauss defaults new databases to A (Oracle)
  revoke_public_connect = true
  enable_private_object = true
}

# Broker-parity grants inside the tenant database: the group role can use
# and extend the shared public schema, and create opt-in schemas.
resource "gaussdb_grant" "public_schema" {
  database    = gaussdb_database.tenant_db.name
  schema      = "public"
  object_type = "schema"
  privileges  = ["USAGE", "CREATE"]
  role        = gaussdb_role.tenant_grp.name
}

resource "gaussdb_grant" "create_schemas" {
  database    = gaussdb_database.tenant_db.name
  object_type = "database"
  privileges  = ["CREATE"]
  role        = gaussdb_role.tenant_grp.name
}

# The application login: member of the group role, read-write.
resource "gaussdb_role" "app" {
  name     = "${var.tenant}_app"
  login    = true
  password = var.app_password
  roles    = [gaussdb_role.tenant_grp.name]
}

output "database" {
  value = gaussdb_database.tenant_db.name
}

output "group_role" {
  value = gaussdb_role.tenant_grp.name
}

output "app_role" {
  value = gaussdb_role.app.name
}
```

Apply it:

```sh
terraform apply \
  -var db_host=10.0.0.99 -var tenant=orders \
  -var db_admin_password=... -var app_password=...
```

## OpenTofu

Works with OpenTofu as-is (verified with 1.13). Two differences: OpenTofu
resolves `alex-tw-lam/gaussdb` to **registry.opentofu.org**, so mirror
layouts and `include` filters need that hostname alongside
`registry.terraform.io`; and the CLI config lives in `~/.tofurc` (the
`TF_CLI_CONFIG_FILE` env var is not honoured).

## License

MPL-2.0, as upstream.
