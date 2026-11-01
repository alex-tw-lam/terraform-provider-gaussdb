package gaussdb

import (
	"context"
	"database/sql"
	"database/sql/driver"
	"net"

	gaussdb "github.com/HuaweiCloudDeveloper/gaussdb-go"
	"github.com/HuaweiCloudDeveloper/gaussdb-go/stdlib"
	"golang.org/x/net/proxy"
)

// proxyDriverName registers the gaussdb stdlib driver with a dialer that
// honors SOCKS/HTTP proxies from the environment. It replaces the former
// lib/pq DialOpen-based proxy driver; DSNs must be gaussdb:// URLs (the
// provider rewrites postgres:// DSNs before they get here).
const proxyDriverName = "gaussdb-proxy"

type proxyDriver struct{}

func (d proxyDriver) Open(name string) (driver.Conn, error) {
	config, err := gaussdb.ParseConfig(name)
	if err != nil {
		return nil, err
	}
	// Route raw connections through the environment proxy dialer when one is
	// configured, and leave host resolution to it (as lib/pq's proxy dialer
	// did) so SOCKS5 remote DNS keeps working. ConnectTimeout still applies:
	// gaussdbconn bounds the whole connect process at the context level.
	dialer := proxy.FromEnvironment()
	config.DialFunc = func(ctx context.Context, network, addr string) (net.Conn, error) {
		if cd, ok := dialer.(proxy.ContextDialer); ok {
			return cd.DialContext(ctx, network, addr)
		}
		return dialer.Dial(network, addr)
	}
	config.LookupFunc = func(ctx context.Context, host string) ([]string, error) {
		return []string{host}, nil
	}
	return stdlib.GetConnector(*config).Connect(context.Background())
}

func init() {
	sql.Register(proxyDriverName, proxyDriver{})
}
