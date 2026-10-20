package operator

import (
	"strings"
	"testing"
)

const good = `{"format":"v0","listen":"127.0.0.1:8443","tls":null,"database":"control.db","routing_file":"spec/routing.json","master_key_credential":"briolink-master-key","adapters":{"codehost":"fake","coder":"fake","reasoner":"fake"}}`

func TestDecode(t *testing.T) {
	c, err := Decode([]byte(good))
	if err != nil {
		t.Fatal(err)
	}
	if c.Listen != "127.0.0.1:8443" || c.Adapters.CodeHost != "fake" {
		t.Errorf("config = %+v", c)
	}
	for name, tc := range map[string]struct{ in, want string }{
		"format":           {strings.Replace(good, `"v0"`, `"v2"`, 1), `format: must be "v0"`},
		"no port":          {strings.Replace(good, "127.0.0.1:8443", "127.0.0.1", 1), "must be an address and a port"},
		"hostname":         {strings.Replace(good, "127.0.0.1:8443", "ops.example.com:8443", 1), "must be an IP address"},
		"wildcard v4":      {strings.Replace(good, "127.0.0.1:8443", "0.0.0.0:8443", 1), "would listen publicly"},
		"wildcard v6":      {strings.Replace(good, "127.0.0.1:8443", "[::]:8443", 1), "would listen publicly"},
		"port zero":        {strings.Replace(good, "8443", "0", 1), "port must be 1 to 65535"},
		"port high":        {strings.Replace(good, "8443", "65536", 1), "port must be 1 to 65535"},
		"private no tls":   {strings.Replace(good, "127.0.0.1", "10.77.0.1", 1), "tls: cert_file and key_file are required"},
		"half a tls":       {strings.Replace(good, `"tls":null`, `"tls":{"cert_file":"c.pem","key_file":""}`, 1), "cert_file and key_file"},
		"blank database":   {strings.Replace(good, `"control.db"`, `""`, 1), "database: must be one non-empty line"},
		"blank credential": {strings.Replace(good, `"briolink-master-key"`, `""`, 1), "master_key_credential: must be one non-empty line"},
		"blank adapter":    {strings.Replace(good, `"coder":"fake"`, `"coder":""`, 1), "adapters.coder: must be one non-empty line"},
		"unknown field":    {strings.Replace(good, `"tls"`, `"token":"x","tls"`, 1), `unknown field "token"`},
	} {
		t.Run(name, func(t *testing.T) {
			_, err := Decode([]byte(tc.in))
			if err == nil || !strings.Contains(err.Error(), tc.want) {
				t.Fatalf("err = %v, want it to contain %q", err, tc.want)
			}
		})
	}
}

func TestPrivateAddressWithTLS(t *testing.T) {
	in := strings.Replace(strings.Replace(good, "127.0.0.1", "10.77.0.1", 1), `"tls":null`, `"tls":{"cert_file":"c.pem","key_file":"k.pem"}`, 1)
	if _, err := Decode([]byte(in)); err != nil {
		t.Fatal(err)
	}
}
