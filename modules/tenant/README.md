# tenant module

Provisions one broker-style GaussDB tenant: a NOLOGIN group (owner) role, a
tablespace with a resizable storage quota, a database with compatibility and
isolation options, the broker-parity grants inside it, and an application
login role. Mirrors the osb-opengauss-go broker's tenant blueprint.

Configure the `gaussdb` provider in your root module, then:

```hcl
module "orders" {
  source = "github.com/alex-tw-lam/terraform-provider-gaussdb//modules/tenant"

  tenant       = "orders"
  app_password = var.orders_app_password # required
  # storage_quota, connection_limit, db_compatibility,
  # revoke_public_connect, enable_private_object all have defaults
}
```

| Output | Value |
| --- | --- |
| `database` | tenant database name |
| `group_role` | tenant group (owner) role |
| `app_role` | application login role |
| `tablespace` | tenant tablespace name |

The group role's password is auto-generated and not stored in state — it is
irrelevant for a NOLOGIN role and cannot be output.
