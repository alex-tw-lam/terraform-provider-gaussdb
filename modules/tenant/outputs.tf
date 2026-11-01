output "database" {
  description = "Tenant database name"
  value       = gaussdb_database.tenant_db.name
}

output "group_role" {
  description = "Tenant group (owner) role"
  value       = gaussdb_role.tenant_grp.name
}

output "app_role" {
  description = "Application login role"
  value       = gaussdb_role.app.name
}

output "tablespace" {
  description = "Tenant tablespace name"
  value       = gaussdb_tablespace.tenant_ts.name
}

output "app_username" {
  description = "Application login role name"
  value       = gaussdb_role.app.name
}

output "app_password" {
  description = "Application login password (as configured)"
  value       = var.app_password
  sensitive   = true
}

output "hosts" {
  description = "Comma-separated seed list (host:port) of the cluster; empty when seed_hosts is unset"
  value       = local.seed_list
}

output "app_uri" {
  description = "Single-endpoint connection URI for the application role; empty when seed_hosts is unset"
  value = local.first_endpoint == "" ? "" : (
    "postgresql://${urlencode(gaussdb_role.app.name)}:${urlencode(var.app_password)}@${local.first_endpoint}/${var.tenant}?sslmode=${var.sslmode}"
  )
  sensitive = true
}

output "uri_seed" {
  description = "Multi-node seed-list URI (target_session_attrs=primary) for HA clients; empty when seed_hosts is unset"
  value = local.seed_list == "" ? "" : (
    "postgresql://${urlencode(gaussdb_role.app.name)}:${urlencode(var.app_password)}@${local.seed_list}/${var.tenant}?target_session_attrs=primary&sslmode=${var.sslmode}"
  )
  sensitive = true
}

output "jdbc_uri" {
  description = "GaussDB JDBC seed-list URI (targetServerType=master); empty when seed_hosts is unset"
  value = local.seed_list == "" ? "" : (
    "jdbc:gaussdb://${local.seed_list}/${var.tenant}?targetServerType=master"
  )
  sensitive = true
}
