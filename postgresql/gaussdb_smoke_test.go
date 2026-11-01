package postgresql

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
	var encryptionType string
	if err := db.QueryRow("SHOW password_encryption_type").Scan(&encryptionType); err != nil {
		t.Fatalf("SHOW password_encryption_type failed: %v", err)
	}
	t.Logf("connected: version=%q password_encryption_type=%q", version, encryptionType)
	if encryptionType != "2" {
		t.Logf("NOTE: password_encryption_type=%q — sha256 login is exercised with type 2", encryptionType)
	}
}
