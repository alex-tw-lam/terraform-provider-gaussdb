package postgresql

// resource_postgresql_tablespace.go manages tablespaces on PostgreSQL and
// openGauss/GaussDB. The openGauss dialect adds RELATIVE LOCATION and MAXSIZE
// (a resizable storage quota) — the clauses the osb-opengauss-go broker uses
// to give every tenant database its quota. On vanilla PostgreSQL only the
// standard name/owner/location triple is supported (no quota concept).

import (
	"database/sql"
	"fmt"
	"log"
	"strings"

	"github.com/hashicorp/terraform-plugin-sdk/v2/helper/schema"
)

const (
	tbsNameAttr             = "name"
	tbsOwnerAttr            = "owner"
	tbsLocationAttr         = "location"
	tbsRelativeLocationAttr = "relative_location"
	tbsMaxSizeAttr          = "maxsize"
)

func resourcePostgreSQLTablespace() *schema.Resource {
	return &schema.Resource{
		Create: PGResourceFunc(resourcePostgreSQLTablespaceCreate),
		Read:   PGResourceFunc(resourcePostgreSQLTablespaceRead),
		Update: PGResourceFunc(resourcePostgreSQLTablespaceUpdate),
		Delete: PGResourceFunc(resourcePostgreSQLTablespaceDelete),
		Exists: PGResourceExistsFunc(resourcePostgreSQLTablespaceExists),
		Importer: &schema.ResourceImporter{
			StateContext: schema.ImportStatePassthroughContext,
		},

		Schema: map[string]*schema.Schema{
			tbsNameAttr: {
				Type:        schema.TypeString,
				Required:    true,
				ForceNew:    true,
				Description: "The name of the tablespace",
			},
			tbsOwnerAttr: {
				Type:        schema.TypeString,
				Optional:    true,
				Computed:    true,
				Description: "The role which owns the tablespace",
			},
			tbsLocationAttr: {
				Type:          schema.TypeString,
				Optional:      true,
				ForceNew:      true,
				ConflictsWith: []string{tbsRelativeLocationAttr},
				Description:   "Absolute directory that will hold the tablespace (must already exist on the server). Portable across flavors; the server catalog does not expose it, so the attribute is write-only",
			},
			tbsRelativeLocationAttr: {
				Type:          schema.TypeString,
				Optional:      true,
				ForceNew:      true,
				ConflictsWith: []string{tbsLocationAttr},
				Description:   "openGauss/GaussDB only: location relative to the environment's tablespace directory",
			},
			tbsMaxSizeAttr: {
				Type:        schema.TypeString,
				Optional:    true,
				Description: "openGauss/GaussDB only: storage quota of the tablespace, e.g. \"100G\" or \"UNLIMITED\"; changing it resizes the quota in place",
				DiffSuppressFunc: func(k, old, new string, d *schema.ResourceData) bool {
					// The server normalizes "1G" into "1048576 K";
					// compare by byte value.
					return equivalentMaxSize(old, new)
				},
			},
		},
	}
}

func resourcePostgreSQLTablespaceCreate(db *DBConnection, d *schema.ResourceData) error {
	tbsName := d.Get(tbsNameAttr).(string)
	owner := d.Get(tbsOwnerAttr).(string)
	location := d.Get(tbsLocationAttr).(string)
	relativeLocation := d.Get(tbsRelativeLocationAttr).(string)
	maxSize := d.Get(tbsMaxSizeAttr).(string)

	if location == "" && relativeLocation == "" {
		return fmt.Errorf("one of location or relative_location is required to create tablespace %q", tbsName)
	}
	if !db.isGaussDB() && (relativeLocation != "" || maxSize != "") {
		return fmt.Errorf("relative_location and maxsize are only supported on openGauss/GaussDB servers")
	}

	var err error
	if owner != "" {
		// ALTER/CREATE TABLESPACE OWNER requires membership in the owner
		// role unless the connecting user is a superuser.
		ownerGranted, err := grantRoleMembership(db, owner, db.client.config.getDatabaseUsername())
		if err != nil {
			return err
		}
		if ownerGranted {
			defer func() {
				_, _ = revokeRoleMembership(db, owner, db.client.config.getDatabaseUsername())
			}()
		}
	}

	b := &strings.Builder{}
	fmt.Fprintf(b, "CREATE TABLESPACE %s", pqQuoteIdentifier(tbsName))
	if owner != "" {
		fmt.Fprintf(b, " OWNER %s", pqQuoteIdentifier(owner))
	}
	if location != "" {
		fmt.Fprintf(b, " LOCATION '%s'", pqQuoteLiteral(location))
	} else {
		fmt.Fprintf(b, " RELATIVE LOCATION '%s'", pqQuoteLiteral(relativeLocation))
		if db.isGaussDB() && maxSize != "" {
			fmt.Fprintf(b, " MAXSIZE '%s'", pqQuoteLiteral(strings.ToUpper(maxSize)))
		}
	}

	if _, err = db.Exec(b.String()); err != nil {
		return fmt.Errorf("error creating tablespace %q: %w", tbsName, err)
	}

	d.SetId(tbsName)

	return resourcePostgreSQLTablespaceReadImpl(db, d)
}

func resourcePostgreSQLTablespaceRead(db *DBConnection, d *schema.ResourceData) error {
	return resourcePostgreSQLTablespaceReadImpl(db, d)
}

func resourcePostgreSQLTablespaceReadImpl(db *DBConnection, d *schema.ResourceData) error {
	tbsName := d.Id()

	var owner string
	if db.isGaussDB() {
		var maxSize sql.NullString
		var relative bool
		// spcmaxsize/relative are openGauss-only first-class columns.
		err := db.QueryRow(
			`SELECT pg_catalog.pg_get_userbyid(spcowner), spcmaxsize, relative
			FROM pg_catalog.pg_tablespace WHERE spcname = $1`, tbsName,
		).Scan(&owner, &maxSize, &relative)
		switch {
		case err == sql.ErrNoRows:
			log.Printf("[WARN] PostgreSQL tablespace (%s) not found", tbsName)
			d.SetId("")
			return nil
		case err != nil:
			return fmt.Errorf("error reading tablespace: %w", err)
		}

		if maxSize.Valid {
			d.Set(tbsMaxSizeAttr, maxSize.String)
		}
		// Only report the relative location shape when the server agrees;
		// the path itself is not recoverable from the catalog.
		if !relative {
			d.Set(tbsRelativeLocationAttr, "")
		}
	} else {
		err := db.QueryRow(
			`SELECT pg_catalog.pg_get_userbyid(spcowner)
			FROM pg_catalog.pg_tablespace WHERE spcname = $1`, tbsName,
		).Scan(&owner)
		switch {
		case err == sql.ErrNoRows:
			log.Printf("[WARN] PostgreSQL tablespace (%s) not found", tbsName)
			d.SetId("")
			return nil
		case err != nil:
			return fmt.Errorf("error reading tablespace: %w", err)
		}
	}

	d.Set(tbsNameAttr, tbsName)
	d.Set(tbsOwnerAttr, owner)

	return nil
}

func resourcePostgreSQLTablespaceUpdate(db *DBConnection, d *schema.ResourceData) error {
	tbsName := d.Get(tbsNameAttr).(string)

	if d.HasChange(tbsOwnerAttr) {
		owner := d.Get(tbsOwnerAttr).(string)
		if owner != "" {
			currentUser := db.client.config.getDatabaseUsername()
			ownerGranted, err := grantRoleMembership(db, owner, currentUser)
			if err != nil {
				return err
			}
			if ownerGranted {
				defer func() {
					_, _ = revokeRoleMembership(db, owner, currentUser)
				}()
			}

			sql := fmt.Sprintf("ALTER TABLESPACE %s OWNER TO %s", pqQuoteIdentifier(tbsName), pqQuoteIdentifier(owner))
			if _, err := db.Exec(sql); err != nil {
				return fmt.Errorf("error updating tablespace OWNER: %w", err)
			}
		}
	}

	if d.HasChange(tbsMaxSizeAttr) {
		if !db.isGaussDB() {
			return fmt.Errorf("maxsize is only supported on openGauss/GaussDB servers")
		}
		maxSize := strings.ToUpper(d.Get(tbsMaxSizeAttr).(string))
		if maxSize == "" {
			maxSize = "UNLIMITED"
		}
		sql := fmt.Sprintf("ALTER TABLESPACE %s RESIZE MAXSIZE '%s'", pqQuoteIdentifier(tbsName), pqQuoteLiteral(maxSize))
		if _, err := db.Exec(sql); err != nil {
			return fmt.Errorf("error resizing tablespace MAXSIZE: %w", err)
		}
	}

	return resourcePostgreSQLTablespaceReadImpl(db, d)
}

func resourcePostgreSQLTablespaceDelete(db *DBConnection, d *schema.ResourceData) error {
	tbsName := d.Get(tbsNameAttr).(string)

	sql := fmt.Sprintf("DROP TABLESPACE %s", pqQuoteIdentifier(tbsName))
	if _, err := db.Exec(sql); err != nil {
		return fmt.Errorf("error dropping tablespace %q: %w", tbsName, err)
	}

	d.SetId("")
	return nil
}

func resourcePostgreSQLTablespaceExists(db *DBConnection, d *schema.ResourceData) (bool, error) {
	tbsName := d.Id()

	var unused int
	err := db.QueryRow(`SELECT 1 FROM pg_catalog.pg_tablespace WHERE spcname = $1`, tbsName).Scan(&unused)
	switch {
	case err == sql.ErrNoRows:
		return false, nil
	case err != nil:
		return false, fmt.Errorf("error checking tablespace %q: %w", tbsName, err)
	}

	return true, nil
}
