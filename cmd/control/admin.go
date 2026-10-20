package main

import (
	"bytes"
	"context"
	"errors"
	"flag"
	"fmt"
	"io"
	"strings"
	"time"

	"github.com/briolink/briolink/control/events"
	"github.com/briolink/briolink/control/expiry"
	"github.com/briolink/briolink/control/operator"
	"github.com/briolink/briolink/control/secrets"
	"github.com/briolink/briolink/control/server"
	"github.com/briolink/briolink/control/store"
	"github.com/briolink/briolink/control/tokens"
)

const adminUsage = `usage: control [-config operator.json] <command>

  secret put NAME [-expires YYYY-MM-DD] [-remind YYYY-MM-DD]    value on stdin
  secret rotate NAME [-expires YYYY-MM-DD] [-remind YYYY-MM-DD] value on stdin
  secret delete NAME
  secret list
  token issue NAME [-scope operator] [-expires YYYY-MM-DD] [-remind YYYY-MM-DD]
  token revoke NAME
  token list`

// admin runs one local command on the database, as the person on the ops host
// who can read the master key. Each is audited like an API call would be. A
// secret's value is read from stdin, never from the command line, and no
// command shows one. The API has no write route yet, and the first token has
// to come from somewhere: the control center brings the API routes (P3).
func admin(ctx context.Context, cfg *operator.Config, args []string, getenv func(string) string, in io.Reader, out io.Writer) error {
	if len(args) < 2 || (args[0] != "secret" && args[0] != "token") {
		return errors.New(adminUsage)
	}
	db, err := store.Open(ctx, cfg.Database, events.New(), time.Now)
	if err != nil {
		return err
	}
	defer db.Close()
	actor := "local:" + orDefault(getenv("USER"), "unknown")
	if args[0] == "token" {
		return tokenCommand(ctx, tokens.New(db.DB(), db, time.Now, []string{server.Role}), actor, args[1:], out)
	}
	key, err := secrets.LoadMasterKey(getenv("CREDENTIALS_DIRECTORY"), cfg.MasterKeyCredential)
	if err != nil {
		return err
	}
	vault, err := secrets.New(db.DB(), key, db, time.Now)
	if err != nil {
		return err
	}
	return secretCommand(ctx, vault, actor, args[1:], in, out)
}

func secretCommand(ctx context.Context, vault *secrets.Vault, actor string, args []string, in io.Reader, out io.Writer) error {
	switch args[0] {
	case "list":
		list, err := vault.List(ctx)
		if err != nil {
			return err
		}
		now := time.Now()
		for _, s := range list {
			fmt.Fprintf(out, "%s\t%s\texpires %s\tremind %s\n", s.Name, s.Expiry.At(now), day(s.Expiry.Expires), day(s.Expiry.Remind))
		}
		return nil
	case "delete":
		name, _, err := nameAndDates("secret delete", args[1:], false)
		if err != nil {
			return err
		}
		if err := vault.Delete(ctx, actor, name); err != nil {
			return err
		}
		fmt.Fprintf(out, "deleted %s\n", name)
		return nil
	case "put", "rotate":
		name, d, err := nameAndDates("secret "+args[0], args[1:], true)
		if err != nil {
			return err
		}
		raw, err := io.ReadAll(io.LimitReader(in, 64<<10+2))
		if err != nil {
			return err
		}
		value := bytes.TrimSuffix(bytes.TrimSuffix(raw, []byte("\n")), []byte("\r"))
		if args[0] == "put" {
			err = vault.Put(ctx, actor, name, value, d)
		} else {
			err = vault.Rotate(ctx, actor, name, value, d)
		}
		if err != nil {
			return err
		}
		fmt.Fprintf(out, "%s %s\n", map[string]string{"put": "stored", "rotate": "rotated"}[args[0]], name)
		return nil
	}
	return errors.New(adminUsage)
}

func tokenCommand(ctx context.Context, svc *tokens.Service, actor string, args []string, out io.Writer) error {
	switch args[0] {
	case "list":
		list, err := svc.List(ctx)
		if err != nil {
			return err
		}
		now := time.Now()
		for _, t := range list {
			state := string(t.Expiry.At(now))
			if t.RevokedAt != nil {
				state = "revoked"
			}
			fmt.Fprintf(out, "%s\tscope %s\t%s\texpires %s\tremind %s\n", t.Name, t.Scope, state, day(t.Expiry.Expires), day(t.Expiry.Remind))
		}
		return nil
	case "revoke":
		name, _, err := nameAndDates("token revoke", args[1:], false)
		if err != nil {
			return err
		}
		if err := svc.Revoke(ctx, actor, name); err != nil {
			return err
		}
		fmt.Fprintf(out, "revoked %s\n", name)
		return nil
	case "issue":
		fs := flag.NewFlagSet("token issue", flag.ContinueOnError)
		scope := fs.String("scope", server.Role, "the role the token acts as")
		expires, remind := dateFlags(fs)
		name, err := parse(fs, args[1:])
		if err != nil {
			return err
		}
		d, err := dates(*expires, *remind)
		if err != nil {
			return err
		}
		token, err := svc.Issue(ctx, actor, name, *scope, d)
		if err != nil {
			return err
		}
		fmt.Fprintf(out, "%s\n\nThis is the only time the token is shown; keep it where the caller reads it.\n", token)
		return nil
	}
	return errors.New(adminUsage)
}

// nameAndDates reads "NAME [-expires D] [-remind D]". With withDates false the
// date flags are not accepted.
func nameAndDates(cmd string, args []string, withDates bool) (string, expiry.Dates, error) {
	fs := flag.NewFlagSet(cmd, flag.ContinueOnError)
	var expires, remind *string
	if withDates {
		expires, remind = dateFlags(fs)
	}
	name, err := parse(fs, args)
	if err != nil {
		return "", expiry.Dates{}, err
	}
	if !withDates {
		return name, expiry.Dates{}, nil
	}
	d, err := dates(*expires, *remind)
	return name, d, err
}

func dateFlags(fs *flag.FlagSet) (expires, remind *string) {
	return fs.String("expires", "", "the day the credential stops being valid, YYYY-MM-DD"),
		fs.String("remind", "", "the day a rotation reminder is due, YYYY-MM-DD")
}

// parse accepts the name before or after the flags.
func parse(fs *flag.FlagSet, args []string) (string, error) {
	var name string
	if len(args) > 0 && !strings.HasPrefix(args[0], "-") {
		name, args = args[0], args[1:]
	}
	if err := fs.Parse(args); err != nil {
		return "", err
	}
	rest := fs.Args()
	if name == "" && len(rest) == 1 {
		name, rest = rest[0], nil
	}
	if name == "" || len(rest) > 0 {
		return "", fmt.Errorf("%s needs one NAME\n%s", fs.Name(), adminUsage)
	}
	return name, nil
}

func dates(expires, remind string) (expiry.Dates, error) {
	var d expiry.Dates
	var err error
	if d.Expires, err = day2time("-expires", expires); err != nil {
		return d, err
	}
	if d.Remind, err = day2time("-remind", remind); err != nil {
		return d, err
	}
	return d, d.Validate()
}

func day2time(flagName, s string) (*time.Time, error) {
	if s == "" {
		return nil, nil
	}
	t, err := time.Parse(time.DateOnly, s)
	if err != nil {
		return nil, fmt.Errorf("%s must be a day like 2027-10-17, got %q", flagName, s)
	}
	return &t, nil
}

func day(t *time.Time) string {
	if t == nil {
		return "-"
	}
	return t.UTC().Format(time.DateOnly)
}

func orDefault(a, b string) string {
	if a != "" {
		return a
	}
	return b
}

// activeTokens counts the tokens that can still be used today.
func activeTokens(ctx context.Context, svc *tokens.Service) (int, error) {
	list, err := svc.List(ctx)
	if err != nil {
		return 0, err
	}
	n := 0
	for _, t := range list {
		if t.RevokedAt == nil && t.Expiry.At(time.Now()) != expiry.Expired {
			n++
		}
	}
	return n, nil
}
