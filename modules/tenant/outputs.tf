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
