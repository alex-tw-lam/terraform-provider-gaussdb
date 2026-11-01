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

variable "connection_limit" {
  description = "CONNECTION LIMIT of the tenant database"
  type        = number
  default     = 64
}

variable "db_compatibility" {
  description = "openGauss DBCOMPATIBILITY of the database (PG/A/B/C). openGauss defaults new databases to A (Oracle), so PG is the safe default"
  type        = string
  default     = "PG"
}

variable "revoke_public_connect" {
  description = "Drop PUBLIC's default CONNECT on the tenant database (tenant isolation)"
  type        = bool
  default     = true
}

variable "enable_private_object" {
  description = "Run ALTER DATABASE … ENABLE PRIVATE OBJECT (openGauss object isolation)"
  type        = bool
  default     = true
}
