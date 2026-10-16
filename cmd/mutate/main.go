// Command mutate is the mutation check: it mutates operators, boundaries and
// constants in the functions of one package, reruns the package's tests and
// reports every mutant they miss. Exit status 0: all caught; 1: survivors;
// 2: the check could not run.
//
//	mutate ./domain/acme -func settles_balance
package main

import (
	"context"
	"flag"
	"fmt"
	"io"
	"os"
	"os/signal"

	"github.com/briolink/briolink/quality/mutate"
)

func main() { os.Exit(run(os.Args[1:], os.Stdout, os.Stderr)) }

func run(args []string, stdout, stderr io.Writer) int {
	fs := flag.NewFlagSet("mutate", flag.ContinueOnError)
	fs.SetOutput(stderr)
	fn := fs.String("func", "", "function to mutate (default: every function in the package)")
	workers := fs.Int("workers", 0, "mutants tried at once (default: half the CPUs)")
	timeout := fs.Duration("timeout", 0, "per mutant (default: 5x the unmutated run, at least 10s)")
	// The directory comes first, as in the contract's verification command.
	var dir string
	if len(args) > 0 && len(args[0]) > 0 && args[0][0] != '-' {
		dir, args = args[0], args[1:]
	}
	if err := fs.Parse(args); err != nil {
		return 2
	}
	if dir == "" && fs.NArg() == 1 {
		dir = fs.Arg(0)
	} else if fs.NArg() != 0 || dir == "" {
		fmt.Fprintln(stderr, "usage: mutate <dir> [-func name] [-workers n] [-timeout d]")
		return 2
	}

	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt)
	defer stop()
	rep, err := mutate.Run(ctx, mutate.Options{Dir: dir, Func: *fn, Workers: *workers, Timeout: *timeout})
	if err != nil {
		fmt.Fprintln(stderr, "mutate:", err)
		return 2
	}

	for _, s := range rep.Survivors() {
		fmt.Fprintf(stdout, "SURVIVED %s\n", s.Mutant)
	}
	killed := rep.Count(mutate.Killed) + rep.Count(mutate.TimedOut)
	fmt.Fprintf(stdout, "%d mutants: %d killed (%d timed out), %d survived, %d invalid\n",
		len(rep.Results), killed, rep.Count(mutate.TimedOut), len(rep.Survivors()), rep.Count(mutate.Invalid))
	if len(rep.Survivors()) > 0 {
		return 1
	}
	return 0
}
