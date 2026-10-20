// Package operator is format v0 of operator.json, the installation's config
// for the control plane (docs/2-architecture.md § Roles and adapters): where
// it listens, where its files are and which adapter fills each port.
// Secrets are not in it; the file names the systemd credential that holds the
// master key of the secret store.
package operator

import (
	"fmt"
	"net"
	"net/netip"
	"os"
	"strconv"

	"github.com/briolink/briolink/core/check"
	"github.com/briolink/briolink/core/strictjson"
)

// Format is the only value Config.Format accepts.
const Format = "v0"

// Config is operator.json.
type Config struct {
	Format string `json:"format"`
	// Listen is the private address the API binds to, an IP literal and a
	// port. A wildcard address is refused: nothing listens publicly (A20).
	Listen string `json:"listen"`
	// TLS is required unless Listen is a loopback address.
	TLS *TLS `json:"tls"`
	// Database is the path of the control plane's SQLite file (E2).
	Database string `json:"database"`
	// RoutingFile is the path of spec/routing.json.
	RoutingFile string `json:"routing_file"`
	// MasterKeyCredential names the systemd credential, a root-only file in
	// $CREDENTIALS_DIRECTORY, that holds the secret store's master key.
	MasterKeyCredential string   `json:"master_key_credential"`
	Adapters            Adapters `json:"adapters"`
}

// TLS is a certificate and its key, as files.
type TLS struct {
	CertFile string `json:"cert_file"`
	KeyFile  string `json:"key_file"`
}

// Adapters names the adapter filling each port the control plane wires.
type Adapters struct {
	CodeHost string `json:"codehost"`
	Coder    string `json:"coder"`
	Reasoner string `json:"reasoner"`
}

// Decode decodes data strictly and validates it.
func Decode(data []byte) (*Config, error) {
	var c Config
	if err := strictjson.Decode(data, &c); err != nil {
		return nil, err
	}
	if err := c.Validate(); err != nil {
		return nil, err
	}
	return &c, nil
}

// Load reads and decodes an operator.json.
func Load(path string) (*Config, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, err
	}
	c, err := Decode(data)
	if err != nil {
		return nil, fmt.Errorf("%s:\n%w", path, err)
	}
	return c, nil
}

// Validate reports every rule the config breaks. Whether an adapter name is
// known is the composition root's to say.
func (c *Config) Validate() error {
	var l check.List
	if c.Format != Format {
		l.Addf("format", "must be %q, got %q", Format, c.Format)
	}
	addr, err := listenAddr(c.Listen)
	if err != nil {
		l.Addf("listen", "%v", err)
	} else if !addr.IsLoopback() && (c.TLS == nil || c.TLS.CertFile == "" || c.TLS.KeyFile == "") {
		l.Addf("tls", "cert_file and key_file are required unless listen is a loopback address")
	}
	if c.TLS != nil && (!check.Text(c.TLS.CertFile) || !check.Text(c.TLS.KeyFile)) {
		l.Addf("tls", "cert_file and key_file must each be a path on one line")
	}
	for name, v := range map[string]string{
		"database": c.Database, "routing_file": c.RoutingFile, "master_key_credential": c.MasterKeyCredential,
		"adapters.codehost": c.Adapters.CodeHost, "adapters.coder": c.Adapters.Coder, "adapters.reasoner": c.Adapters.Reasoner,
	} {
		if !check.Text(v) {
			l.Addf(name, "must be one non-empty line")
		}
	}
	return l.Err()
}

func listenAddr(s string) (netip.Addr, error) {
	host, port, err := net.SplitHostPort(s)
	if err != nil {
		return netip.Addr{}, fmt.Errorf("must be an address and a port like 10.0.0.1:8443, got %q", s)
	}
	addr, err := netip.ParseAddr(host)
	if err != nil {
		return netip.Addr{}, fmt.Errorf("the host must be an IP address, got %q", host)
	}
	if addr.IsUnspecified() {
		return netip.Addr{}, fmt.Errorf("a wildcard address %q would listen publicly; name the private address", host)
	}
	if n, err := strconv.Atoi(port); err != nil || n < 1 || n > 65535 {
		return netip.Addr{}, fmt.Errorf("the port must be 1 to 65535, got %q", port)
	}
	return addr, nil
}
