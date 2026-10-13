// Command control is the composition root for the control plane: it will
// wire ports to their adapters from the installation's operator config and
// serve the control plane's HTTP JSON API and event stream. Ports, adapters
// and the API land in later P0 issues; for now it reports its own version.
package main

import (
	"fmt"
	"os"
)

// version is set at build time via -ldflags "-X main.version=...".
var version = "dev"

func main() {
	fmt.Fprintf(os.Stdout, "briolink control %s\n", version)
}
