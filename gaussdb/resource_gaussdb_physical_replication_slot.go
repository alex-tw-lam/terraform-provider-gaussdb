package gaussdb

import (
	"database/sql"
	"fmt"
	"github.com/hashicorp/terraform-plugin-sdk/v2/helper/schema"
)

func resourceGaussDBPhysicalReplicationSlot() *schema.Resource {
	return &schema.Resource{
		Create: GaussDBResourceFunc(resourceGaussDBPhysicalReplicationSlotCreate),
		Read:   GaussDBResourceFunc(resourceGaussDBPhysicalReplicationSlotRead),
		Delete: GaussDBResourceFunc(resourceGaussDBPhysicalReplicationSlotDelete),
		Exists: GaussDBResourceExistsFunc(resourceGaussDBPhysicalReplicationSlotExists),
		Importer: &schema.ResourceImporter{
			StateContext: schema.ImportStatePassthroughContext,
		},

		Schema: map[string]*schema.Schema{
			"name": {
				Type:     schema.TypeString,
				Required: true,
				ForceNew: true,
			},
		},
	}
}

func resourceGaussDBPhysicalReplicationSlotCreate(db *DBConnection, d *schema.ResourceData) error {
	name := d.Get("name").(string)
	// NB: bare "SELECT FROM func()" is PostgreSQL-only syntax; openGauss
	// rejects it. "SELECT func(...)" is portable to both. openGauss's
	// variant additionally takes dummy_standby (false = a regular physical
	// slot; true is for dummy standby log recycling).
	sql := "SELECT pg_create_physical_replication_slot($1)"
	if db.isGaussDB() {
		sql = "SELECT pg_create_physical_replication_slot($1, false)"
	}
	if _, err := db.Exec(sql, name); err != nil {
		return fmt.Errorf("could not create physical ReplicationSlot %s: %w", name, err)
	}
	d.SetId(name)

	return nil
}

func resourceGaussDBPhysicalReplicationSlotExists(db *DBConnection, d *schema.ResourceData) (bool, error) {
	query := "SELECT 1 FROM pg_catalog.pg_replication_slots WHERE slot_name = $1 and slot_type = 'physical'"
	var unused int
	err := db.QueryRow(query, d.Id()).Scan(&unused)
	switch {
	case err == sql.ErrNoRows:
		return false, nil
	case err != nil:
		return false, err
	}

	return true, nil
}

func resourceGaussDBPhysicalReplicationSlotRead(db *DBConnection, d *schema.ResourceData) error {
	d.Set("name", d.Id())
	return nil
}

func resourceGaussDBPhysicalReplicationSlotDelete(db *DBConnection, d *schema.ResourceData) error {

	replicationSlotName := d.Get("name").(string)

	if _, err := db.Exec("SELECT pg_drop_replication_slot($1)", replicationSlotName); err != nil {
		return err
	}

	d.SetId("")
	return nil
}
