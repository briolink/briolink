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
	"syscall"
	"time"

	"github.com/briolink/briolink/cmd/internal/wire"
	"github.com/briolink/briolink/control/events"
	"github.com/briolink/briolink/control/logic"
	"github.com/briolink/briolink/control/operator"
	"github.com/briolink/briolink/control/routing"
	"github.com/briolink/briolink/control/server"
	"github.com/briolink/briolink/control/service"
	"github.com/briolink/briolink/control/store"
	"github.com/briolink/briolink/control/vault"
)

// version is set at build time via -ldflags "-X main.version=...".
var version = "dev"

func main() {
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()
	if err := run(ctx, os.Args[1:], os.Getenv, os.Stdin, os.Stdout, nil); err != nil {
		fmt.Fprintln(os.Stderr, "control:", err)
		os.Exit(1)
	}
}

// run serves until ctx ends, or, given a command (secret, token,
// credentials), runs it and returns. ready, when not nil, is called with the address
// the server listens on once it accepts connections.
func run(ctx context.Context, args []string, getenv func(string) string, in io.Reader, out io.Writer, ready func(addr string)) error {
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
	key, err := wire.LoadKey(getenv, cfg.MasterKeyCredential)
	if err != nil {
		return err
	}
	command := fs.Args()
	if len(command) == 0 {
		// Refuse a bad binding before touching the database; nothing is
		// used yet, so no secret is needed.
		noSecret := func(context.Context, string, string) ([]byte, error) {
			return nil, errors.New("the secret store is not open")
		}
		if _, err = wire.All(wire.Deps{Cfg: cfg, Table: table, Secret: noSecret}); err != nil {
			return err
		}
	}

	broker := events.New()
	db, err := store.Open(ctx, cfg.Database, broker, time.Now)
	if err != nil {
		return err
	}
	defer db.Close()
	vlt, err := vault.New(db.DB(), key, db, server.Roles, time.Now)
	if err != nil {
		return err
	}
	// The key must be the one the stored secrets were sealed with, before the
	// service starts and before a command seals a new one under it. Deleting
	// a secret, revoking a token and listing need no key.
	if len(command) == 0 || (command[0] == "secret" && len(command) > 1 && command[1] != "delete") {
		if err := vlt.Verify(ctx); err != nil {
			return fmt.Errorf("master key %s: %w", cfg.MasterKeyCredential, err)
		}
	}
	if len(command) > 0 {
		return admin(ctx, vlt, command, in, out)
	}
	p, err := wire.All(wire.Deps{Cfg: cfg, Table: table, Secret: vlt.Use})
	if err != nil {
		return err
	}

	handler := server.Handler(server.Config{
		Service: &service.Service{Routing: table, Host: p.CodeHost, Records: db, Credentials: vlt, Funcs: logic.Impl{}},
		Events:  broker,
		Tokens:  vlt,
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
