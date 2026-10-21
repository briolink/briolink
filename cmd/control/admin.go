package main

import (
	"bytes"
	"context"
	"errors"
	"flag"
	"fmt"
	"io"
	"text/tabwriter"
	"time"

	"github.com/briolink/briolink/control/logic"
	"github.com/briolink/briolink/control/server"
	"github.com/briolink/briolink/control/vault"
	"github.com/briolink/briolink/gen/domain/control"
)

// cliActor is the name the audit log carries for a command run on the host.
const cliActor = "cli"

// now is the clock of the credential-expiry check; tests replace it.
var now = time.Now

const adminUsage = `commands:
  secret put NAME [-expires D] [-remind D]     value from standard input
  secret rotate NAME [-expires D] [-remind D]  value from standard input
  secret delete NAME
  token create NAME [-role operator] [-expires D] [-remind D]   prints the token once
  token revoke NAME
  credentials [-expiring | -check]             names and dates, never values; -check is
                                               the daily credential-expiry check
  (dates are YYYY-MM-DD)`

// admin runs one command of the secret store. It writes to the same database
// as a running control plane, whose event stream does not see these audit
// lines until it is asked for them through /api/v1/audit.
func admin(ctx context.Context, v *vault.Vault, args []string, in io.Reader, out io.Writer) error {
	usage := func() error { return errors.New(adminUsage) }
	switch args[0] {
	case "secret":
		if len(args) < 3 {
			return usage()
		}
		verb, name := args[1], args[2]
		fs := flag.NewFlagSet("secret "+verb, flag.ContinueOnError)
		var d vault.Dates
		fs.StringVar(&d.ExpiresOn, "expires", "", "expiry date")
		fs.StringVar(&d.RemindOn, "remind", "", "reminder date")
		if err := fs.Parse(args[3:]); err != nil {
			return err
		}
		switch verb {
		case "put", "rotate":
			value, err := io.ReadAll(io.LimitReader(in, 1<<20))
			if err != nil {
				return err
			}
			value = bytes.TrimSuffix(value, []byte("\n"))
			if verb == "put" {
				err = v.Put(ctx, cliActor, name, value, d)
			} else {
				err = v.Rotate(ctx, cliActor, name, value, d)
			}
			if err != nil {
				return err
			}
			fmt.Fprintf(out, "secret %s: %s\n", name, map[string]string{"put": "stored", "rotate": "rotated"}[verb])
			return nil
		case "delete":
			if err := v.Delete(ctx, cliActor, name); err != nil {
				return err
			}
			fmt.Fprintf(out, "secret %s: deleted\n", name)
			return nil
		}
		return usage()
	case "token":
		if len(args) < 3 {
			return usage()
		}
		verb, name := args[1], args[2]
		fs := flag.NewFlagSet("token "+verb, flag.ContinueOnError)
		var d vault.Dates
		role := fs.String("role", server.Role, "the role the token acts as")
		fs.StringVar(&d.ExpiresOn, "expires", "", "expiry date")
		fs.StringVar(&d.RemindOn, "remind", "", "reminder date")
		if err := fs.Parse(args[3:]); err != nil {
			return err
		}
		switch verb {
		case "create":
			token, err := v.CreateToken(ctx, cliActor, name, *role, d)
			if err != nil {
				return err
			}
			fmt.Fprintln(out, token)
			return nil
		case "revoke":
			if err := v.RevokeToken(ctx, cliActor, name); err != nil {
				return err
			}
			fmt.Fprintf(out, "token %s: revoked\n", name)
			return nil
		}
		return usage()
	case "credentials":
		fs := flag.NewFlagSet("credentials", flag.ContinueOnError)
		expiring := fs.Bool("expiring", false, "only those whose reminder has come or that expire within 30 days")
		check := fs.Bool("check", false, "report the credentials that are remind or expired")
		if err := fs.Parse(args[1:]); err != nil {
			return err
		}
		if *check {
			if *expiring {
				return usage()
			}
			return checkCredentials(ctx, v, out)
		}
		list := v.List
		if *expiring {
			list = v.Expiring
		}
		entries, err := list(ctx)
		if err != nil {
			return err
		}
		tw := tabwriter.NewWriter(out, 0, 4, 2, ' ', 0)
		fmt.Fprintln(tw, "KIND\tNAME\tROLE\tEXPIRES\tREMIND\tSTATE")
		for _, e := range entries {
			state := ""
			if e.Revoked {
				state = "revoked"
			}
			fmt.Fprintf(tw, "%s\t%s\t%s\t%s\t%s\t%s\n", e.Kind, e.Name, e.Role, e.ExpiresOn, e.RemindOn, state)
		}
		return tw.Flush()
	}
	return usage()
}

// checkCredentials is the daily credential-expiry check: one line per
// credential that is remind or expired, with its name and expiry date, never a
// value. A day's run with nothing to report says so.
func checkCredentials(ctx context.Context, v *vault.Vault, out io.Writer) error {
	entries, err := v.List(ctx)
	if err != nil {
		return err
	}
	all := make([]control.Credential, len(entries))
	for i, e := range entries {
		all[i] = control.Credential{Kind: e.Kind, Name: e.Name, Role: e.Role, CreatedAt: e.CreatedAt, RotatedAt: e.RotatedAt,
			ExpiresOn: e.ExpiresOn, RemindOn: e.RemindOn, Revoked: e.Revoked}
	}
	alerts := logic.Impl{}.CredentialAlerts(all, now().UTC().Format("2006-01-02"))
	if len(alerts) == 0 {
		fmt.Fprintln(out, "credentials: nothing to report")
		return nil
	}
	for _, a := range alerts {
		fmt.Fprintf(out, "credentials: %s %s %s expires %s\n", a.State, a.Kind, a.Name, a.ExpiresOn)
	}
	return nil
}
