package postgresql

import (
	"bytes"
	"database/sql"
	"errors"
	"fmt"
	"log"
	"strings"

	"github.com/hashicorp/terraform-plugin-sdk/v2/helper/schema"
	"github.com/hashicorp/terraform-plugin-sdk/v2/helper/validation"
)

const (
	dbAllowConnsAttr       = "allow_connections"
	dbCTypeAttr            = "lc_ctype"
	dbCollationAttr        = "lc_collate"
	dbCompatibilityAttr    = "db_compatibility"
	dbConnLimitAttr        = "connection_limit"
	dbEncodingAttr         = "encoding"
	dbIsTemplateAttr       = "is_template"
	dbNameAttr             = "name"
	dbOwnerAttr            = "owner"
	dbTablespaceAttr       = "tablespace_name"
	dbTemplateAttr         = "template"
	dbAlterObjectOwnership = "alter_object_ownership"

	dbRevokePublicConnectAttr = "revoke_public_connect"
	dbEnablePrivateObjectAttr = "enable_private_object"
)

func resourcePostgreSQLDatabase() *schema.Resource {
	return &schema.Resource{
		Create: PGResourceFunc(resourcePostgreSQLDatabaseCreate),
		Read:   PGResourceFunc(resourcePostgreSQLDatabaseRead),
		Update: PGResourceFunc(resourcePostgreSQLDatabaseUpdate),
		Delete: PGResourceFunc(resourcePostgreSQLDatabaseDelete),
		Exists: PGResourceExistsFunc(resourcePostgreSQLDatabaseExists),
		Importer: &schema.ResourceImporter{
			StateContext: schema.ImportStatePassthroughContext,
		},

		Schema: map[string]*schema.Schema{
			dbNameAttr: {
				Type:        schema.TypeString,
				Required:    true,
				Description: "The PostgreSQL database name to connect to",
			},
			dbOwnerAttr: {
				Type:        schema.TypeString,
				Optional:    true,
				Computed:    true,
				Description: "The ROLE which owns the database",
			},
			dbTemplateAttr: {
				Type:        schema.TypeString,
				Optional:    true,
				ForceNew:    true,
				Computed:    true,
				Description: "The name of the template from which to create the new database",
			},
			dbEncodingAttr: {
				Type:        schema.TypeString,
				Optional:    true,
				Computed:    true,
				ForceNew:    true,
				Description: "Character set encoding to use in the new database",
			},
			dbCollationAttr: {
				Type:        schema.TypeString,
				Optional:    true,
				Computed:    true,
				ForceNew:    true,
				Description: "Collation order (LC_COLLATE) to use in the new database",
			},
			dbCTypeAttr: {
				Type:        schema.TypeString,
				Optional:    true,
				Computed:    true,
				ForceNew:    true,
				Description: "Character classification (LC_CTYPE) to use in the new database",
			},
			dbTablespaceAttr: {
				Type:        schema.TypeString,
				Optional:    true,
				Computed:    true,
				Description: "The name of the tablespace that will be associated with the new database",
			},
			dbConnLimitAttr: {
				Type:         schema.TypeInt,
				Optional:     true,
				Default:      -1,
				Description:  "How many concurrent connections can be made to this database",
				ValidateFunc: validation.IntAtLeast(-1),
			},
			dbAllowConnsAttr: {
				Type:        schema.TypeBool,
				Optional:    true,
				Default:     true,
				Description: "If false then no one can connect to this database",
			},
			dbIsTemplateAttr: {
				Type:        schema.TypeBool,
				Optional:    true,
				Computed:    true,
				Description: "If true, then this database can be cloned by any user with CREATEDB privileges",
			},
			dbAlterObjectOwnership: {
				Type:        schema.TypeBool,
				Optional:    true,
				Default:     false,
				Description: "If true, the owner of already existing objects will change if the owner changes",
			},
			dbCompatibilityAttr: {
				Type:         schema.TypeString,
				Optional:     true,
				ForceNew:     true,
				Description:  "openGauss/GaussDB DBCOMPATIBILITY of the new database: PG, A (Oracle), B (MySQL) or C (Teradata). Only \"PG\" is accepted against vanilla PostgreSQL (where it is a no-op). NB: openGauss defaults new databases to A (Oracle) when unset.",
				ValidateFunc: validation.StringInSlice([]string{"PG", "A", "B", "C"}, true),
			},
			dbRevokePublicConnectAttr: {
				Type:        schema.TypeBool,
				Optional:    true,
				Default:     false,
				Description: "If true, PUBLIC loses its default CONNECT privilege on this database (tenant isolation; plain SQL on all flavors)",
			},
			dbEnablePrivateObjectAttr: {
				Type:        schema.TypeBool,
				Optional:    true,
				Default:     false,
				Description: "openGauss/GaussDB only: run ALTER DATABASE ... ENABLE PRIVATE OBJECT so users only see objects they may access. Not supported on vanilla PostgreSQL; the server exposes no readable flag, so drift on this attribute cannot be detected",
			},
		},
	}
}

func resourcePostgreSQLDatabaseCreate(db *DBConnection, d *schema.ResourceData) error {
	if err := createDatabase(db, d); err != nil {
		return err
	}

	d.SetId(d.Get(dbNameAttr).(string))

	return resourcePostgreSQLDatabaseReadImpl(db, d)
}

func createDatabase(db *DBConnection, d *schema.ResourceData) error {
	currentUser := db.client.config.getDatabaseUsername()
	owner := d.Get(dbOwnerAttr).(string)

	var err error
	if owner != "" {
		// Take a lock on db currentUser to avoid multiple database creation at the same time
		// It can fail if they grant the same owner to current at the same time as it's not done in transaction.
		lockTxn, err := startTransaction(db.client, "")
		if err != nil {
			return err
		}
		if err := pgLockRole(lockTxn, currentUser); err != nil {
			return err
		}
		defer deferredRollback(lockTxn)

		// Needed in order to set the owner of the db if the connection user is not a
		// superuser
		ownerGranted, err := grantRoleMembership(db, owner, currentUser)
		if err != nil {
			return err
		}
		if ownerGranted {
			defer func() {
				_, err = revokeRoleMembership(db, owner, currentUser)
			}()
		}
	}

	dbName := d.Get(dbNameAttr).(string)
	b := bytes.NewBufferString("CREATE DATABASE ")
	fmt.Fprint(b, pqQuoteIdentifier(dbName))

	// Handle each option individually and stream results into the query
	// buffer.
	switch v, ok := d.GetOk(dbOwnerAttr); {
	case ok:
		fmt.Fprint(b, " OWNER ", pqQuoteIdentifier(v.(string)))
	default:
		// No owner specified in the config, default to using
		// the connecting username.
		fmt.Fprint(b, " OWNER ", pqQuoteIdentifier(currentUser))
	}

	switch v, ok := d.GetOk(dbTemplateAttr); {
	case ok && strings.ToUpper(v.(string)) == "DEFAULT":
		fmt.Fprint(b, " TEMPLATE DEFAULT")
	case ok:
		fmt.Fprint(b, " TEMPLATE ", pqQuoteIdentifier(v.(string)))
	case v.(string) == "":
		fmt.Fprint(b, " TEMPLATE template0")
	}

	switch v, ok := d.GetOk(dbEncodingAttr); {
	case ok && strings.ToUpper(v.(string)) == "DEFAULT":
		fmt.Fprintf(b, " ENCODING DEFAULT")
	case ok:
		fmt.Fprintf(b, " ENCODING '%s' ", pqQuoteLiteral(v.(string)))
	case v.(string) == "":
		fmt.Fprint(b, ` ENCODING 'UTF8'`)
	}

	// Don't specify LC_COLLATE if user didn't specify it
	// This will use the default one (usually the one defined in the template database)
	switch v, ok := d.GetOk(dbCollationAttr); {
	case ok && strings.ToUpper(v.(string)) == "DEFAULT":
		fmt.Fprintf(b, " LC_COLLATE DEFAULT")
	case ok:
		fmt.Fprintf(b, " LC_COLLATE '%s' ", pqQuoteLiteral(v.(string)))
	}

	// Don't specify LC_CTYPE if user didn't specify it
	// This will use the default one (usually the one defined in the template database)
	switch v, ok := d.GetOk(dbCTypeAttr); {
	case ok && strings.ToUpper(v.(string)) == "DEFAULT":
		fmt.Fprintf(b, " LC_CTYPE DEFAULT")
	case ok:
		fmt.Fprintf(b, " LC_CTYPE '%s' ", pqQuoteLiteral(v.(string)))
	}

	switch v, ok := d.GetOk(dbTablespaceAttr); {
	case ok && strings.ToUpper(v.(string)) == "DEFAULT":
		fmt.Fprint(b, " TABLESPACE DEFAULT")
	case ok:
		fmt.Fprint(b, " TABLESPACE ", pqQuoteIdentifier(v.(string)))
	}

	if db.featureSupported(featureDBAllowConnections) {
		val := d.Get(dbAllowConnsAttr).(bool)
		fmt.Fprint(b, " ALLOW_CONNECTIONS ", val)
	}

	{
		val := d.Get(dbConnLimitAttr).(int)
		fmt.Fprint(b, " CONNECTION LIMIT ", val)
	}

	if db.featureSupported(featureDBIsTemplate) {
		val := d.Get(dbIsTemplateAttr).(bool)
		fmt.Fprint(b, " IS_TEMPLATE ", val)
	}

	if compat, ok := d.GetOk(dbCompatibilityAttr); ok {
		v := strings.ToUpper(compat.(string))
		if db.isGaussDB() {
			fmt.Fprintf(b, " DBCOMPATIBILITY '%s'", pqQuoteLiteral(v))
		} else if v != "PG" {
			return fmt.Errorf("db_compatibility %q is only supported on openGauss/GaussDB servers; only \"PG\" is portable to PostgreSQL", v)
		}
	}

	sql := b.String()
	if _, err := db.Exec(sql); err != nil {
		return fmt.Errorf("error creating database %q: %w", dbName, err)
	}

	if d.Get(dbRevokePublicConnectAttr).(bool) {
		revokeSQL := fmt.Sprintf("REVOKE CONNECT ON DATABASE %s FROM PUBLIC", pqQuoteIdentifier(dbName))
		if _, err := db.Exec(revokeSQL); err != nil {
			return fmt.Errorf("error revoking CONNECT from PUBLIC on database %q: %w", dbName, err)
		}
	}

	if d.Get(dbEnablePrivateObjectAttr).(bool) {
		if !db.isGaussDB() {
			return fmt.Errorf("enable_private_object is only supported on openGauss/GaussDB servers")
		}
		if err := setPrivateObject(db, dbName, true); err != nil {
			return err
		}
	}

	// Set err outside of the return so that the deferred revoke can override err
	// if necessary.
	return err
}

func resourcePostgreSQLDatabaseDelete(db *DBConnection, d *schema.ResourceData) error {
	currentUser := db.client.config.getDatabaseUsername()
	owner := d.Get(dbOwnerAttr).(string)

	var dropWithForce string
	var err error
	if owner != "" {
		lockTxn, err := startTransaction(db.client, "")
		if err := pgLockRole(lockTxn, currentUser); err != nil {
			return err
		}
		defer deferredRollback(lockTxn)

		// Needed in order to set the owner of the db if the connection user is not a
		// superuser
		ownerGranted, err := grantRoleMembership(db, owner, currentUser)
		if err != nil {
			return err
		}
		if ownerGranted {
			defer func() {
				_, err = revokeRoleMembership(db, owner, currentUser)
			}()
		}
	}

	dbName := d.Get(dbNameAttr).(string)
	if db.featureSupported(featureDBIsTemplate) {
		if isTemplate := d.Get(dbIsTemplateAttr).(bool); isTemplate {
			// Template databases must have this attribute cleared before
			// they can be dropped.
			if err := doSetDBIsTemplate(db, dbName, false); err != nil {
				return fmt.Errorf("error updating database IS_TEMPLATE during DROP DATABASE: %w", err)
			}
		}
	}

	if err := setDBIsTemplate(db, d); err != nil {
		return err
	}

	// Terminate all active connections and block new one
	if err := terminateBConnections(db, dbName); err != nil {
		return err
	}

	// Drop with force only for psql 13+
	if db.featureSupported(featureForceDropDatabase) {
		dropWithForce = "WITH ( FORCE )"
	}

	sql := fmt.Sprintf("DROP DATABASE %s %s", pqQuoteIdentifier(dbName), dropWithForce)
	if _, err := db.Exec(sql); err != nil {
		return fmt.Errorf("error dropping database: %w", err)
	}

	d.SetId("")

	// Returning err even if it's nil so defer func can modify it.
	return err
}

func resourcePostgreSQLDatabaseExists(db *DBConnection, d *schema.ResourceData) (bool, error) {
	txn, err := startTransaction(db.client, "")
	if err != nil {
		return false, err
	}
	defer deferredRollback(txn)

	return dbExists(txn, d.Id())
}

func resourcePostgreSQLDatabaseRead(db *DBConnection, d *schema.ResourceData) error {
	return resourcePostgreSQLDatabaseReadImpl(db, d)
}

func resourcePostgreSQLDatabaseReadImpl(db *DBConnection, d *schema.ResourceData) error {
	dbId := d.Id()
	var dbName, ownerName string
	err := db.QueryRow("SELECT d.datname, pg_catalog.pg_get_userbyid(d.datdba) from pg_database d WHERE datname=$1", dbId).Scan(&dbName, &ownerName)
	switch {
	case err == sql.ErrNoRows:
		log.Printf("[WARN] PostgreSQL database (%q) not found", dbId)
		d.SetId("")
		return nil
	case err != nil:
		return fmt.Errorf("error reading database: %w", err)
	}

	var dbEncoding, dbCollation, dbCType, dbTablespaceName string
	var dbConnLimit int

	columns := []string{
		"pg_catalog.pg_encoding_to_char(d.encoding)",
		"d.datcollate",
		"d.datctype",
		"ts.spcname",
		"d.datconnlimit",
	}

	dbSQLFmt := `SELECT %s ` +
		`FROM pg_catalog.pg_database AS d, pg_catalog.pg_tablespace AS ts ` +
		`WHERE d.datname = $1 AND d.dattablespace = ts.oid`
	dbSQL := fmt.Sprintf(dbSQLFmt, strings.Join(columns, ", "))
	err = db.QueryRow(dbSQL, dbId).
		Scan(
			&dbEncoding,
			&dbCollation,
			&dbCType,
			&dbTablespaceName,
			&dbConnLimit,
		)
	switch {
	case err == sql.ErrNoRows:
		log.Printf("[WARN] PostgreSQL database (%q) not found", dbId)
		d.SetId("")
		return nil
	case err != nil:
		return fmt.Errorf("error reading database: %w", err)
	}

	d.Set(dbNameAttr, dbName)
	d.Set(dbOwnerAttr, ownerName)
	d.Set(dbEncodingAttr, dbEncoding)
	d.Set(dbCollationAttr, dbCollation)
	d.Set(dbCTypeAttr, dbCType)
	d.Set(dbTablespaceAttr, dbTablespaceName)
	d.Set(dbConnLimitAttr, dbConnLimit)
	dbTemplate := d.Get(dbTemplateAttr).(string)
	if dbTemplate == "" {
		dbTemplate = "template0"
	}
	d.Set(dbTemplateAttr, dbTemplate)

	if db.featureSupported(featureDBAllowConnections) {
		var dbAllowConns bool
		dbSQL := fmt.Sprintf(dbSQLFmt, "d.datallowconn")
		err = db.QueryRow(dbSQL, dbId).Scan(&dbAllowConns)
		if err != nil {
			return fmt.Errorf("error reading ALLOW_CONNECTIONS property for DATABASE: %w", err)
		}

		d.Set(dbAllowConnsAttr, dbAllowConns)
	}

	if db.featureSupported(featureDBIsTemplate) {
		var dbIsTemplate bool
		dbSQL := fmt.Sprintf(dbSQLFmt, "d.datistemplate")
		err = db.QueryRow(dbSQL, dbId).Scan(&dbIsTemplate)
		if err != nil {
			return fmt.Errorf("error reading IS_TEMPLATE property for DATABASE: %w", err)
		}

		d.Set(dbIsTemplateAttr, dbIsTemplate)
	}

	if db.isGaussDB() {
		var dbCompatibility string
		if err := db.QueryRow(`SELECT d.datcompatibility FROM pg_catalog.pg_database d WHERE d.datname = $1`, dbId).Scan(&dbCompatibility); err != nil {
			return fmt.Errorf("error reading DBCOMPATIBILITY property for DATABASE: %w", err)
		}
		d.Set(dbCompatibilityAttr, dbCompatibility)
	}

	// PUBLIC's CONNECT: a NULL datacl means defaults (PUBLIC may connect);
	// once materialized, count the PUBLIC (grantee 0) CONNECT entries.
	// Plain aclexplode scalar subquery — openGauss has no LATERAL.
	var dataclNull bool
	var publicConnectCount int
	err = db.QueryRow(`SELECT d.datacl IS NULL, coalesce((SELECT count(*) FROM aclexplode(d.datacl) WHERE grantee = 0 AND privilege_type = 'CONNECT'), 0)
		FROM pg_catalog.pg_database d WHERE d.datname = $1`, dbId).Scan(&dataclNull, &publicConnectCount)
	if err != nil {
		return fmt.Errorf("error reading CONNECT privilege of PUBLIC for DATABASE: %w", err)
	}
	d.Set(dbRevokePublicConnectAttr, !dataclNull && publicConnectCount == 0)

	// enable_private_object is write-only on openGauss: the server exposes
	// no readable flag, so the state keeps whatever the config says.

	return nil
}

func resourcePostgreSQLDatabaseUpdate(db *DBConnection, d *schema.ResourceData) error {
	if err := setDBName(db, d); err != nil {
		return err
	}

	if err := setAlterOwnership(db, d); err != nil {
		return err
	}

	if err := setDBOwner(db, d); err != nil {
		return err
	}

	if err := setDBTablespace(db, d); err != nil {
		return err
	}

	if err := setDBConnLimit(db, d); err != nil {
		return err
	}

	if err := setDBAllowConns(db, d); err != nil {
		return err
	}

	if err := setDBIsTemplate(db, d); err != nil {
		return err
	}

	if err := setRevokePublicConnect(db, d); err != nil {
		return err
	}

	if err := setEnablePrivateObject(db, d); err != nil {
		return err
	}

	// Empty values: ALTER DATABASE name RESET configuration_parameter;

	return resourcePostgreSQLDatabaseReadImpl(db, d)
}

// setRevokePublicConnect applies or restores PUBLIC's default CONNECT
// privilege on the database.
func setRevokePublicConnect(db *DBConnection, d *schema.ResourceData) error {
	if !d.HasChange(dbRevokePublicConnectAttr) {
		return nil
	}

	revoke := d.Get(dbRevokePublicConnectAttr).(bool)
	dbName := d.Get(dbNameAttr).(string)
	var sql string
	if revoke {
		sql = fmt.Sprintf("REVOKE CONNECT ON DATABASE %s FROM PUBLIC", pqQuoteIdentifier(dbName))
	} else {
		sql = fmt.Sprintf("GRANT CONNECT ON DATABASE %s TO PUBLIC", pqQuoteIdentifier(dbName))
	}
	if _, err := db.Exec(sql); err != nil {
		return fmt.Errorf("error updating CONNECT privilege of PUBLIC on database %q: %w", dbName, err)
	}

	return nil
}

// setEnablePrivateObject toggles openGauss's per-database private-object
// isolation.
func setEnablePrivateObject(db *DBConnection, d *schema.ResourceData) error {
	if !d.HasChange(dbEnablePrivateObjectAttr) {
		return nil
	}

	enabled := d.Get(dbEnablePrivateObjectAttr).(bool)
	if !db.isGaussDB() {
		if !enabled {
			// Was never turned on; nothing to disable.
			return nil
		}
		return fmt.Errorf("enable_private_object is only supported on openGauss/GaussDB servers")
	}

	return setPrivateObject(db, d.Get(dbNameAttr).(string), enabled)
}

// setPrivateObject runs ALTER DATABASE ... ENABLE/DISABLE PRIVATE OBJECT.
// openGauss refuses this statement cross-database, so it must execute on a
// connection to the target database itself.
func setPrivateObject(db *DBConnection, dbName string, enabled bool) error {
	target, err := initConnection(db.client, dbName)
	if err != nil {
		return err
	}
	action := "DISABLE"
	if enabled {
		action = "ENABLE"
	}
	sql := fmt.Sprintf("ALTER DATABASE %s %s PRIVATE OBJECT", pqQuoteIdentifier(dbName), action)
	if _, err := target.Exec(sql); err != nil {
		return fmt.Errorf("error running %s PRIVATE OBJECT on database %q: %w", action, dbName, err)
	}

	return nil
}

func setDBName(db QueryAble, d *schema.ResourceData) error {
	if !d.HasChange(dbNameAttr) {
		return nil
	}

	oraw, nraw := d.GetChange(dbNameAttr)
	o := oraw.(string)
	n := nraw.(string)
	if n == "" {
		return errors.New("error setting database name to an empty string")
	}

	sql := fmt.Sprintf("ALTER DATABASE %s RENAME TO %s", pqQuoteIdentifier(o), pqQuoteIdentifier(n))
	if _, err := db.Exec(sql); err != nil {
		return fmt.Errorf("error updating database name: %w", err)
	}
	d.SetId(n)

	return nil
}

func setDBOwner(db *DBConnection, d *schema.ResourceData) error {
	if !d.HasChange(dbOwnerAttr) {
		return nil
	}

	owner := d.Get(dbOwnerAttr).(string)
	if owner == "" {
		return nil
	}
	currentUser := db.client.config.getDatabaseUsername()

	lockTxn, err := startTransaction(db.client, "")
	if err := pgLockRole(lockTxn, currentUser); err != nil {
		return err
	}
	defer deferredRollback(lockTxn)

	//needed in order to set the owner of the db if the connection user is not a superuser
	ownerGranted, err := grantRoleMembership(db, owner, currentUser)
	if err != nil {
		return err
	}
	if ownerGranted {
		defer func() {
			_, err = revokeRoleMembership(db, owner, currentUser)
		}()
	}

	dbName := d.Get(dbNameAttr).(string)

	sql := fmt.Sprintf("ALTER DATABASE %s OWNER TO %s", pqQuoteIdentifier(dbName), pqQuoteIdentifier(owner))
	if _, err := db.Exec(sql); err != nil {
		return fmt.Errorf("error updating database OWNER: %w", err)
	}

	return err
}

func setAlterOwnership(db *DBConnection, d *schema.ResourceData) error {
	if !d.HasChange(dbOwnerAttr) && !d.HasChange(dbAlterObjectOwnership) {
		return nil
	}
	owner := d.Get(dbOwnerAttr).(string)
	if owner == "" {
		return nil
	}

	alterOwnership := d.Get(dbAlterObjectOwnership).(bool)
	if !alterOwnership {
		return nil
	}
	currentUser := db.client.config.getDatabaseUsername()

	dbName := d.Get(dbNameAttr).(string)

	lockTxn, err := startTransaction(db.client, dbName)
	if err := pgLockRole(lockTxn, currentUser); err != nil {
		return err
	}
	defer deferredRollback(lockTxn)

	currentOwner, err := getDatabaseOwner(db, dbName)
	if err != nil {
		return fmt.Errorf("error getting current database OWNER: %w", err)
	}

	newOwner := d.Get(dbOwnerAttr).(string)

	if currentOwner == newOwner {
		return nil
	}

	currentOwnerGranted, err := grantRoleMembership(db, currentOwner, currentUser)
	if err != nil {
		return err
	}
	if currentOwnerGranted {
		defer func() {
			_, err = revokeRoleMembership(db, currentOwner, currentUser)
		}()
	}
	sql := fmt.Sprintf("REASSIGN OWNED BY %s TO %s", pqQuoteIdentifier(currentOwner), pqQuoteIdentifier(newOwner))
	if _, err := lockTxn.Exec(sql); err != nil {
		return fmt.Errorf("error reassigning objects owned by '%s': %w", currentOwner, err)
	}

	if err := lockTxn.Commit(); err != nil {
		return fmt.Errorf("error committing reassign: %w", err)
	}
	return nil
}

func setDBTablespace(db QueryAble, d *schema.ResourceData) error {
	if !d.HasChange(dbTablespaceAttr) {
		return nil
	}

	tbspName := d.Get(dbTablespaceAttr).(string)
	dbName := d.Get(dbNameAttr).(string)
	var sql string
	if tbspName == "" || strings.ToUpper(tbspName) == "DEFAULT" {
		sql = fmt.Sprintf("ALTER DATABASE %s RESET TABLESPACE", pqQuoteIdentifier(dbName))
	} else {
		sql = fmt.Sprintf("ALTER DATABASE %s SET TABLESPACE %s", pqQuoteIdentifier(dbName), pqQuoteIdentifier(tbspName))
	}

	if _, err := db.Exec(sql); err != nil {
		return fmt.Errorf("error updating database TABLESPACE: %w", err)
	}

	return nil
}

func setDBConnLimit(db QueryAble, d *schema.ResourceData) error {
	if !d.HasChange(dbConnLimitAttr) {
		return nil
	}

	connLimit := d.Get(dbConnLimitAttr).(int)
	dbName := d.Get(dbNameAttr).(string)
	sql := fmt.Sprintf("ALTER DATABASE %s CONNECTION LIMIT = %d", pqQuoteIdentifier(dbName), connLimit)
	if _, err := db.Exec(sql); err != nil {
		return fmt.Errorf("error updating database CONNECTION LIMIT: %w", err)
	}

	return nil
}

func setDBAllowConns(db *DBConnection, d *schema.ResourceData) error {
	if !d.HasChange(dbAllowConnsAttr) {
		return nil
	}

	if !db.featureSupported(featureDBAllowConnections) {
		return fmt.Errorf("PostgreSQL client is talking with a server (%q) that does not support database ALLOW_CONNECTIONS", db.version.String())
	}

	allowConns := d.Get(dbAllowConnsAttr).(bool)
	dbName := d.Get(dbNameAttr).(string)
	sql := fmt.Sprintf("ALTER DATABASE %s ALLOW_CONNECTIONS %t", pqQuoteIdentifier(dbName), allowConns)
	if _, err := db.Exec(sql); err != nil {
		return fmt.Errorf("error updating database ALLOW_CONNECTIONS: %w", err)
	}

	return nil
}

func setDBIsTemplate(db *DBConnection, d *schema.ResourceData) error {
	if !d.HasChange(dbIsTemplateAttr) {
		return nil
	}

	if err := doSetDBIsTemplate(db, d.Get(dbNameAttr).(string), d.Get(dbIsTemplateAttr).(bool)); err != nil {
		return fmt.Errorf("error updating database IS_TEMPLATE: %w", err)
	}

	return nil
}

func doSetDBIsTemplate(db *DBConnection, dbName string, isTemplate bool) error {
	if !db.featureSupported(featureDBIsTemplate) {
		return fmt.Errorf("PostgreSQL client is talking with a server (%q) that does not support database IS_TEMPLATE", db.version.String())
	}

	sql := fmt.Sprintf("ALTER DATABASE %s IS_TEMPLATE %t", pqQuoteIdentifier(dbName), isTemplate)
	if _, err := db.Exec(sql); err != nil {
		return fmt.Errorf("error updating database IS_TEMPLATE: %w", err)
	}

	return nil
}

func terminateBConnections(db *DBConnection, dbName string) error {
	var terminateSql string

	if db.featureSupported(featureDBAllowConnections) {
		alterSql := fmt.Sprintf("ALTER DATABASE %s ALLOW_CONNECTIONS false", pqQuoteIdentifier(dbName))

		if _, err := db.Exec(alterSql); err != nil {
			return fmt.Errorf("error blocking connections to database: %w", err)
		}
	}
	pid := "procpid"
	if db.featureSupported(featurePid) {
		pid = "pid"
	}
	terminateSql = fmt.Sprintf("SELECT pg_terminate_backend(%s) FROM pg_stat_activity WHERE datname = '%s' AND %s <> pg_backend_pid()", pid, dbName, pid)
	if _, err := db.Exec(terminateSql); err != nil {
		return fmt.Errorf("error terminating database connections: %w", err)
	}

	return nil
}
