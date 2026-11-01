package gaussdb

// gaussdb_smoke_test.go exercises the real provider connection path —
// Config.Connect -> connectWithRetry -> proxy driver -> gaussdb stdlib ->
// openGauss sha256 authentication — against a live server. It is opt-in and
// skipped unless GAUSSDB_SMOKE_DSN is set, e.g.:
//
//	GAUSSDB_SMOKE_DSN='postgres://gaussdb:secret@localhost:25432/postgres?sslmode=disable' \
//	  go test ./postgresql/ -run TestGaussDBConnectionSmoke -v

import (
	"net/url"
	"os"
	"strconv"
	"strings"
	"testing"
)

func TestGaussDBConnectionSmoke(t *testing.T) {
	dsn := os.Getenv("GAUSSDB_SMOKE_DSN")
	if dsn == "" {
		t.Skip("GAUSSDB_SMOKE_DSN not set")
	}
	u, err := url.Parse(dsn)
	if err != nil {
		t.Fatalf("cannot parse GAUSSDB_SMOKE_DSN: %v", err)
	}
	password, _ := u.User.Password()
	port, err := strconv.Atoi(u.Port())
	if err != nil || port == 0 {
		port = 5432
	}
	sslmode := u.Query().Get("sslmode")
	if sslmode == "" {
		sslmode = "disable"
	}

	cfg := &Config{
		Scheme:                        "postgres",
		Host:                          u.Hostname(),
		Port:                          port,
		Username:                      u.User.Username(),
		Password:                      password,
		SSLMode:                       sslmode,
		ApplicationName:               "gaussdb-smoke",
		ConnectTimeoutSec:             5,
		MaxConnRetries:                2,
		ConnectionRetryTimeoutSeconds: 30,
		ConnMaxLifetimeSeconds:        60,
		MaxConns:                      2,
	}

	client := cfg.NewClient(strings.TrimPrefix(u.Path, "/"))
	db, err := client.Connect()
	if err != nil {
		t.Fatalf("Connect through the gaussdb driver failed: %v", err)
	}

	var one int
	if err := db.QueryRow("SELECT 1").Scan(&one); err != nil {
		t.Fatalf("SELECT 1 failed: %v", err)
	}
	var version string
	if err := db.QueryRow("SELECT version()").Scan(&version); err != nil {
		t.Fatalf("SELECT version() failed: %v", err)
	}
	// password_encryption_type only exists on openGauss/GaussDB; scanning
	// pg_settings avoids both openGauss's missing two-argument current_setting
	// and NULL scans on vanilla PostgreSQL. password_encryption shows the
	// server-side hash default (scram-sha-256 on modern PostgreSQL).
	encryptionSettings := map[string]string{}
	rows, err := db.Query("SELECT name, setting FROM pg_catalog.pg_settings WHERE name IN ('password_encryption_type', 'password_encryption')")
	if err != nil {
		t.Fatalf("reading password encryption settings failed: %v", err)
	}
	for rows.Next() {
		var name, setting string
		if err := rows.Scan(&name, &setting); err != nil {
			t.Fatalf("scanning password encryption settings failed: %v", err)
		}
		encryptionSettings[name] = setting
	}
	rows.Close()
	t.Logf("connected: version=%q password_encryption_type=%q password_encryption=%q",
		version, encryptionSettings["password_encryption_type"], encryptionSettings["password_encryption"])
	if encryptionSettings["password_encryption_type"] != "2" && encryptionSettings["password_encryption"] != "scram-sha-256" {
		t.Logf("NOTE: server neither uses openGauss sha256 (type 2) nor PostgreSQL scram-sha-256; auth used md5 or cleartext")
	}
}
