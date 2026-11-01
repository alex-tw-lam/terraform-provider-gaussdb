package gaussdb

import (
	"bytes"
	"database/sql"
	"fmt"
	"log"
	"strings"

	"github.com/hashicorp/terraform-plugin-sdk/v2/helper/schema"
)

const (
	serverNameAttr        = "server_name"
	serverTypeAttr        = "server_type"
	serverVersionAttr     = "server_version"
	serverOwnerAttr       = "server_owner"
	serverFDWAttr         = "fdw_name"
	serverOptionsAttr     = "options"
	serverDropCascadeAttr = "drop_cascade"
)

func resourceGaussDBServer() *schema.Resource {
	return &schema.Resource{
		Create: GaussDBResourceFunc(resourceGaussDBServerCreate),
		Read:   GaussDBResourceFunc(resourceGaussDBServerRead),
		Update: GaussDBResourceFunc(resourceGaussDBServerUpdate),
		Delete: GaussDBResourceFunc(resourceGaussDBServerDelete),
		Importer: &schema.ResourceImporter{
			StateContext: schema.ImportStatePassthroughContext,
		},

		Schema: map[string]*schema.Schema{
			serverNameAttr: {
				Type:        schema.TypeString,
				Required:    true,
				Description: "The name of the foreign server to be created",
			},
			serverTypeAttr: {
				Type:        schema.TypeString,
				Optional:    true,
				ForceNew:    true,
				Description: "Optional server type, potentially useful to foreign-data wrappers",
			},
			serverVersionAttr: {
				Type:        schema.TypeString,
				Optional:    true,
				Description: "Optional server version, potentially useful to foreign-data wrappers.",
			},
			serverFDWAttr: {
				Type:        schema.TypeString,
				Required:    true,
				ForceNew:    true,
				Description: "The name of the foreign-data wrapper that manages the server",
			},
			serverOwnerAttr: {
				Type:        schema.TypeString,
				Optional:    true,
				Computed:    true,
				Description: "The user name of the new owner of the foreign server",
			},
			serverOptionsAttr: {
				Type: schema.TypeMap,
				Elem: &schema.Schema{
					Type: schema.TypeString,
				},
				Optional:    true,
				Description: "This clause specifies the options for the server. The options typically define the connection details of the server, but the actual names and values are dependent on the server's foreign-data wrapper",
			},
			serverDropCascadeAttr: {
				Type:        schema.TypeBool,
				Optional:    true,
				Default:     false,
				Description: "Automatically drop objects that depend on the server (such as user mappings), and in turn all objects that depend on those objects. Drop RESTRICT is the default",
			},
		},
	}
}

func resourceGaussDBServerCreate(db *DBConnection, d *schema.ResourceData) error {
	if !db.featureSupported(featureServer) {
		return fmt.Errorf(
			"foreign server resource is not supported for this Postgres version (%s)",
			db.version,
		)
	}

	serverName := d.Get(serverNameAttr).(string)

	b := bytes.NewBufferString("CREATE SERVER ")
	fmt.Fprint(b, pqQuoteIdentifier(serverName))

	if v, ok := d.GetOk(serverTypeAttr); ok {
		fmt.Fprint(b, " TYPE ", quoteLiteral(v.(string)))
	}

	if v, ok := d.GetOk(serverVersionAttr); ok {
		fmt.Fprint(b, " VERSION ", quoteLiteral(v.(string)))
	}

	fmt.Fprint(b, " FOREIGN DATA WRAPPER ", pqQuoteIdentifier(d.Get(serverFDWAttr).(string)))

	if options, ok := d.GetOk(serverOptionsAttr); ok {
		fmt.Fprint(b, " OPTIONS ( ")
		cnt := 0
		len := len(options.(map[string]any))
		for k, v := range options.(map[string]any) {
			fmt.Fprint(b, " ", pqQuoteIdentifier(k), " ", quoteLiteral(v.(string)))
			if cnt < len-1 {
				fmt.Fprint(b, ", ")
			}
			cnt++
		}
		fmt.Fprint(b, " ) ")
	}

	txn, err := startTransaction(db.client, "")
	if err != nil {
		return err
	}
	defer deferredRollback(txn)

	sql := b.String()
	if _, err := txn.Exec(sql); err != nil {
		return err
	}

	if v, ok := d.GetOk(serverOwnerAttr); ok {
		currentUser, err := getCurrentUser(txn)
		if err != nil {
			return err
		}
		if v != currentUser {
			if err := setServerOwner(txn, d); err != nil {
				return err
			}
		}
	}

	if err = txn.Commit(); err != nil {
		return fmt.Errorf("error creating server: %w", err)
	}

	d.SetId(d.Get(serverNameAttr).(string))

	return resourceGaussDBServerReadImpl(db, d)
}

func resourceGaussDBServerRead(db *DBConnection, d *schema.ResourceData) error {
	if !db.featureSupported(featureServer) {
		return fmt.Errorf(
			"foreign server resource is not supported for this Postgres version (%s)",
			db.version,
		)
	}

	return resourceGaussDBServerReadImpl(db, d)
}

func resourceGaussDBServerReadImpl(db *DBConnection, d *schema.ResourceData) error {
	serverName := d.Get(serverNameAttr).(string)
	txn, err := startTransaction(db.client, "")
	if err != nil {
		return err
	}
	defer deferredRollback(txn)

	var serverType, serverVersion, serverOwner, serverFDW string
	var serverOptions pgStringArray
	// NB: pg_get_userbyid() instead of the ::regrole cast, which openGauss
	// does not implement; identical result on PostgreSQL.
	query := `SELECT COALESCE(fs.srvtype, ''), COALESCE(fs.srvversion, ''), pg_get_userbyid(fs.srvowner), fs.srvoptions, w.fdwname ` +
		`FROM pg_foreign_server fs JOIN pg_foreign_data_wrapper w on w.oid = fs.srvfdw ` +
		`WHERE fs.srvname = $1`
	err = txn.QueryRow(query, serverName).Scan(&serverType, &serverVersion, &serverOwner, &serverOptions, &serverFDW)
	switch {
	case err == sql.ErrNoRows:
		log.Printf("[WARN] PostgreSQL foreign server (%s) not found", serverName)
		d.SetId("")
		return nil
	case err != nil:
		return fmt.Errorf("error reading foreign server: %w", err)
	}

	mappedOptions := make(map[string]any)
	for _, v := range serverOptions {
		pair := strings.Split(v, "=")
		mappedOptions[pair[0]] = pair[1]
	}

	d.Set(serverNameAttr, serverName)
	d.Set(serverTypeAttr, serverType)
	d.Set(serverVersionAttr, serverVersion)
	d.Set(serverOwnerAttr, serverOwner)
	d.Set(serverOptionsAttr, mappedOptions)
	d.Set(serverFDWAttr, serverFDW)
	d.SetId(serverName)

	return nil
}

func resourceGaussDBServerDelete(db *DBConnection, d *schema.ResourceData) error {
	if !db.featureSupported(featureServer) {
		return fmt.Errorf(
			"foreign server resource is not supported for this Postgres version (%s)",
			db.version,
		)
	}

	serverName := d.Get(serverNameAttr).(string)

	txn, err := startTransaction(db.client, "")
	if err != nil {
		return err
	}
	defer deferredRollback(txn)

	dropMode := "RESTRICT"
	if d.Get(serverDropCascadeAttr).(bool) {
		dropMode = "CASCADE"
	}

	sql := fmt.Sprintf("DROP SERVER %s %s ", pqQuoteIdentifier(serverName), dropMode)
	if _, err := txn.Exec(sql); err != nil {
		return err
	}

	if err = txn.Commit(); err != nil {
		return fmt.Errorf("error deleting server: %w", err)
	}

	d.SetId("")

	return nil
}

func resourceGaussDBServerUpdate(db *DBConnection, d *schema.ResourceData) error {
	if !db.featureSupported(featureServer) {
		return fmt.Errorf(
			"foreign server resource is not supported for this Postgres version (%s)",
			db.version,
		)
	}

	txn, err := startTransaction(db.client, "")
	if err != nil {
		return err
	}
	defer deferredRollback(txn)

	if err := setServerNameIfChanged(txn, d); err != nil {
		return err
	}

	if err := setServerOwnerIfChanged(txn, d); err != nil {
		return err
	}

	if err := setServerVersionOptionsIfChanged(txn, d); err != nil {
		return err
	}

	if err = txn.Commit(); err != nil {
		return fmt.Errorf("error updating foreign server: %w", err)
	}

	return resourceGaussDBServerReadImpl(db, d)
}

func setServerVersionOptionsIfChanged(txn *sql.Tx, d *schema.ResourceData) error {
	if !d.HasChange(serverVersionAttr) && !d.HasChange(serverOptionsAttr) {
		return nil
	}

	b := bytes.NewBufferString("ALTER SERVER ")
	serverName := d.Get(serverNameAttr).(string)

	fmt.Fprintf(b, "%s ", pqQuoteIdentifier(serverName))

	if d.HasChange(serverVersionAttr) {
		fmt.Fprintf(b, "VERSION %s", quoteLiteral(d.Get(serverVersionAttr).(string)))
	}

	if d.HasChange(serverOptionsAttr) {
		oldOptions, newOptions := d.GetChange(serverOptionsAttr)
		fmt.Fprint(b, " OPTIONS ( ")
		cnt := 0
		len := len(newOptions.(map[string]any))
		toRemove := oldOptions.(map[string]any)
		for k, v := range newOptions.(map[string]any) {
			operation := "ADD"
			if oldOptions.(map[string]any)[k] != nil {
				operation = "SET"
				delete(toRemove, k)
			}
			fmt.Fprintf(b, " %s %s %s ", operation, pqQuoteIdentifier(k), quoteLiteral(v.(string)))
			if cnt < len-1 {
				fmt.Fprint(b, ", ")
			}
			cnt++
		}

		for k := range toRemove {
			if cnt != 0 { // starting with 0 means to drop all the options. Cannot start with comma
				fmt.Fprint(b, " , ")
			}
			fmt.Fprintf(b, " DROP %s ", pqQuoteIdentifier(k))
			cnt++
		}

		fmt.Fprint(b, " ) ")
	}

	sql := b.String()
	if _, err := txn.Exec(sql); err != nil {
		return fmt.Errorf("error updating foreign server version and/or options: %w", err)
	}

	return nil
}

func setServerNameIfChanged(txn *sql.Tx, d *schema.ResourceData) error {
	if !d.HasChange(serverNameAttr) {
		return nil
	}

	serverOldName, serverNewName := d.GetChange(serverNameAttr)

	b := bytes.NewBufferString("ALTER SERVER ")
	fmt.Fprintf(b, "%s RENAME TO %s", pqQuoteIdentifier(serverOldName.(string)), pqQuoteIdentifier(serverNewName.(string)))

	sql := b.String()
	if _, err := txn.Exec(sql); err != nil {
		return fmt.Errorf("error updating foreign server name: %w", err)
	}

	return nil
}

func setServerOwnerIfChanged(txn *sql.Tx, d *schema.ResourceData) error {
	if !d.HasChange(serverOwnerAttr) {
		return nil
	}
	return setServerOwner(txn, d)
}

func setServerOwner(txn *sql.Tx, d *schema.ResourceData) error {
	serverName := d.Get(serverNameAttr).(string)
	serverNewOwner := d.Get(serverOwnerAttr).(string)

	b := bytes.NewBufferString("ALTER SERVER ")
	fmt.Fprintf(b, "%s OWNER TO %s", pqQuoteIdentifier(serverName), pqQuoteIdentifier(serverNewOwner))

	sql := b.String()
	if _, err := txn.Exec(sql); err != nil {
		return fmt.Errorf("error updating foreign server owner: %w", err)
	}

	return nil
}
