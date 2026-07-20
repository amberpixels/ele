// Command ele wraps pg_restore with a live, aggregated progress view.
//
//	ele <pg_restore args>       run a restore live: swallow the verbose stderr
//	                            and repaint per-phase progress + a classified
//	                            error panel; print a summary at the end
//	ele --plan <dump>           offline: print the parsed restore plan only
//	ele --replay <dump> <log>   offline: replay a captured stderr log
package main

import (
	"context"
	"fmt"
	"io"
	"os"

	"github.com/amberpixels/ele/internal/engine"
	"github.com/amberpixels/ele/internal/humanize"
	"github.com/amberpixels/ele/internal/preflight"
)

// version is set at build time via -ldflags "-X main.version=...".
var version = "dev"

func main() {
	code, err := dispatch(os.Args[1:])
	if err != nil {
		fmt.Fprintln(os.Stderr, "ele:", err)
	}
	os.Exit(code)
}

func dispatch(args []string) (int, error) {
	switch {
	case len(args) == 0 || args[0] == "-h" || args[0] == "--help":
		printUsage(os.Stderr)
		return 2, nil

	case args[0] == "--version" || args[0] == "-V":
		fmt.Fprintln(os.Stdout, "ele", version)
		return 0, nil

	case args[0] == "--plan":
		if len(args) != 2 {
			printUsage(os.Stderr)
			return 2, nil
		}
		return fail(preflightOnly(os.Stdout, args[1]))

	case args[0] == "--replay":
		if len(args) != 3 {
			printUsage(os.Stderr)
			return 2, nil
		}
		return engine.Replay(context.Background(), args[1], args[2], os.Stdout, os.Stderr)

	default:
		return engine.Run(context.Background(), args, os.Stdout, os.Stderr)
	}
}

// fail maps an error from an offline mode to an exit code.
func fail(err error) (int, error) {
	if err != nil {
		return 1, err
	}
	return 0, nil
}

func printUsage(w io.Writer) {
	fmt.Fprint(w, "usage:\n"+
		"  ele <pg_restore args>       run a restore live (drop-in pg_restore wrapper)\n"+
		"  ele --plan <plan>           print the parsed restore plan and exit\n"+
		"  ele --replay <plan> <log>   dry-run: replay a captured stderr log through the\n"+
		"                              live view\n"+
		"                              (<plan> is a dump or a saved pg_restore -l listing)\n")
}

func preflightOnly(out io.Writer, dumpPath string) error {
	res, err := preflight.Run(context.Background(), dumpPath)
	if err != nil {
		return err
	}
	printPlan(out, res)
	return nil
}

func printPlan(out io.Writer, res *preflight.Result) {
	plan := res.Plan
	pre, data, post, unknown := plan.PhaseCounts()

	fmt.Fprintf(out, "format:  %s\n", res.Format)
	fmt.Fprintf(out, "entries: %d\n\n", len(plan.Entries))

	fmt.Fprintf(out, "  pre-data   %d\n", pre)
	if res.ByteSized {
		fmt.Fprintf(out, "  data       %d  (%s)\n", data, humanize.Bytes(plan.DataBytes()))
	} else {
		fmt.Fprintf(out, "  data       %d\n", data)
	}
	fmt.Fprintf(out, "  post-data  %d\n", post)
	if unknown > 0 {
		fmt.Fprintf(out, "  unknown    %d\n", unknown)
	}
}
