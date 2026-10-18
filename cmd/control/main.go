// Command control is the composition root for the control plane: it wires
// ports to their adapters from the installation's operator config and serves
// the control plane's HTTP JSON API and event stream (docs/2-architecture.md
// § Control plane). It is the only place that names adapters.
package main

import (
	"context"
	"errors"
	"flag"
	"fmt"
	"io"
	"net"
	"net/http"
	"os"
	"os/signal"
	"sort"
	"strings"
	"syscall"
	"time"

	"github.com/briolink/briolink/control/events"
	"github.com/briolink/briolink/control/logic"
	"github.com/briolink/briolink/control/operator"
	"github.com/briolink/briolink/control/routing"
	"github.com/briolink/briolink/control/server"
	"github.com/briolink/briolink/control/service"
	"github.com/briolink/briolink/control/store"
	"github.com/briolink/briolink/port/codehost"
	"github.com/briolink/briolink/port/coder"
	"github.com/briolink/briolink/port/reasoner"
)

// version is set at build time via -ldflags "-X main.version=...".
var version = "dev"

func main() {
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()
	if err := run(ctx, os.Args[1:], os.Getenv, os.Stdout, nil); err != nil {
		fmt.Fprintln(os.Stderr, "control:", err)
		os.Exit(1)
	}
}

// ports are the adapters the control plane holds. Only the code host is
// served by the API today; the runner (#60) takes the other two.
type ports struct {
	codeHost codehost.Host
	coder    coder.Coder
	reasoner reasoner.Reasoner
}

// The adapters each port can be bound to, by the name operator.json gives.
// Real adapters are added here as they pass their port's contract tests.
var (
	codeHosts = map[string]func() codehost.Host{"fake": func() codehost.Host { return codehost.NewFake() }}
	coders    = map[string]func() coder.Coder{"fake": func() coder.Coder { return &coder.Fake{} }}
	reasoners = map[string]func() reasoner.Reasoner{"fake": func() reasoner.Reasoner { return &reasoner.Fake{} }}
)

func pick[T any](port, name string, from map[string]func() T) (T, error) {
	if f, ok := from[name]; ok {
		return f(), nil
	}
	var zero T
	known := make([]string, 0, len(from))
	for k := range from {
		known = append(known, k)
	}
	sort.Strings(known)
	return zero, fmt.Errorf("adapters.%s: no adapter %q (known: %s)", port, name, strings.Join(known, ", "))
}

func wire(a operator.Adapters) (ports, error) {
	var p ports
	var err error
	if p.codeHost, err = pick("codehost", a.CodeHost, codeHosts); err != nil {
		return p, err
	}
	if p.coder, err = pick("coder", a.Coder, coders); err != nil {
		return p, err
	}
	p.reasoner, err = pick("reasoner", a.Reasoner, reasoners)
	return p, err
}

// run serves until ctx ends. ready, when not nil, is called with the address
// the server listens on once it accepts connections.
func run(ctx context.Context, args []string, getenv func(string) string, out io.Writer, ready func(addr string)) error {
	fs := flag.NewFlagSet("control", flag.ContinueOnError)
	configPath := fs.String("config", "operator.json", "the installation's operator config")
	showVersion := fs.Bool("version", false, "print the version and exit")
	if err := fs.Parse(args); err != nil {
		return err
	}
	if *showVersion {
		fmt.Fprintf(out, "briolink control %s\n", version)
		return nil
	}

	cfg, err := operator.Load(*configPath)
	if err != nil {
		return err
	}
	table, err := routing.Load(cfg.RoutingFile)
	if err != nil {
		return err
	}
	apiToken := getenv(cfg.TokenEnv)
	if apiToken == "" {
		return fmt.Errorf("the environment variable %s (token_env) is empty: the API would accept nobody", cfg.TokenEnv)
	}
	p, err := wire(cfg.Adapters)
	if err != nil {
		return err
	}

	broker := events.New()
	db, err := store.Open(ctx, cfg.Database, broker, time.Now)
	if err != nil {
		return err
	}
	defer db.Close()

	handler := server.Handler(server.Config{
		Service: &service.Service{Routing: table, Host: p.codeHost, Records: db, Funcs: logic.Impl{}},
		Events:  broker,
		Token:   apiToken,
	})
	ln, err := net.Listen("tcp", cfg.Listen)
	if err != nil {
		return err
	}
	// Streams outlive requests; cancelling their base context ends them so
	// the shutdown does not wait on a client that never leaves.
	streams, endStreams := context.WithCancel(context.Background())
	defer endStreams()
	srv := &http.Server{
		Handler:           handler,
		BaseContext:       func(net.Listener) context.Context { return streams },
		ReadHeaderTimeout: 10 * time.Second,
	}

	if _, err := db.AddAudit(ctx, "control", "start", "control plane",
		fmt.Sprintf("version %s; codehost=%s coder=%s reasoner=%s", version, cfg.Adapters.CodeHost, cfg.Adapters.Coder, cfg.Adapters.Reasoner)); err != nil {
		ln.Close()
		return err
	}
	fmt.Fprintf(out, "briolink control %s listening on %s\n", version, ln.Addr())
	if ready != nil {
		ready(ln.Addr().String())
	}

	served := make(chan error, 1)
	go func() {
		if cfg.TLS != nil {
			served <- srv.ServeTLS(ln, cfg.TLS.CertFile, cfg.TLS.KeyFile)
		} else {
			served <- srv.Serve(ln)
		}
	}()
	select {
	case err := <-served:
		return err
	case <-ctx.Done():
	}
	endStreams()
	shut, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	err = srv.Shutdown(shut)
	// The audit line is best effort: the context that ended the run is done.
	if _, aerr := db.AddAudit(shut, "control", "stop", "control plane", ""); aerr != nil {
		err = errors.Join(err, aerr)
	}
	return err
}
