terraform {
  required_providers {
    gaussdb = {
      source = "alex-tw-lam/gaussdb"
    }
  }
}

# The tenant's group role: NOLOGIN owner of everything below. openGauss
# mandates a password on every role, so the provider generates one (and
# deliberately does not store it in state — the value is irrelevant for a
# NOLOGIN role).
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
  connection_limit      = var.connection_limit
  db_compatibility      = var.db_compatibility
  revoke_public_connect = var.revoke_public_connect
  enable_private_object = var.enable_private_object
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
