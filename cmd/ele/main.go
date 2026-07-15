// Command ele wraps pg_restore with a live, aggregated progress view.
//
//	ele <pg_restore args>       run a restore live: swallow the verbose stderr
//	                            and repaint per-phase progress + a classified
//	                            error panel; print a summary at the end
//	ele --plan <dump>           offline: print the parsed restore plan only
//	ele --replay <dump> <log>   offline: replay a captured stderr log
package main

import (
	"bufio"
	"context"
	"fmt"
	"io"
	"os"

	"github.com/amberpixels/ele/internal/aggregator"
	"github.com/amberpixels/ele/internal/engine"
	"github.com/amberpixels/ele/internal/parser"
	"github.com/amberpixels/ele/internal/preflight"
	"github.com/amberpixels/ele/internal/render"
)

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
		return fail(replay(os.Stdout, args[1], args[2]))

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
		"  ele --plan <dump>           print the parsed restore plan and exit\n"+
		"  ele --replay <dump> <log>   replay a captured stderr log and print the summary\n")
}

func preflightOnly(out io.Writer, dumpPath string) error {
	res, err := preflight.Run(context.Background(), dumpPath)
	if err != nil {
		return err
	}
	printPlan(out, res)
	return nil
}

// replay runs preflight for the plan, then feeds a captured stderr log through
// the parser and aggregator and prints the summary. It touches no database.
func replay(out io.Writer, dumpPath, logPath string) error {
	res, err := preflight.Run(context.Background(), dumpPath)
	if err != nil {
		return err
	}
	f, err := os.Open(logPath)
	if err != nil {
		return err
	}
	defer f.Close()

	agg := aggregator.New(res.Plan, aggregator.Config{Clean: true, NoOwner: true})
	p := parser.New()
	sc := bufio.NewScanner(f)
	sc.Buffer(make([]byte, 0, 64*1024), 4*1024*1024)
	for sc.Scan() {
		for _, ev := range p.Feed(sc.Text()) {
			agg.Feed(ev)
		}
	}
	for _, ev := range p.Flush() {
		agg.Feed(ev)
	}
	render.Summary(out, agg.Snapshot(), logPath, 0)
	return nil
}

func printPlan(out io.Writer, res *preflight.Result) {
	plan := res.Plan
	pre, data, post, unknown := plan.PhaseCounts()

	fmt.Fprintf(out, "format:  %s\n", res.Format)
	fmt.Fprintf(out, "entries: %d\n\n", len(plan.Entries))

	fmt.Fprintf(out, "  pre-data   %d\n", pre)
	if res.ByteSized {
		fmt.Fprintf(out, "  data       %d  (%s)\n", data, humanBytes(plan.DataBytes()))
	} else {
		fmt.Fprintf(out, "  data       %d\n", data)
	}
	fmt.Fprintf(out, "  post-data  %d\n", post)
	if unknown > 0 {
		fmt.Fprintf(out, "  unknown    %d\n", unknown)
	}
}

// humanBytes renders a byte count as a short human-readable string.
func humanBytes(n int64) string {
	const unit = 1024
	if n < unit {
		return fmt.Sprintf("%d B", n)
	}
	div, exp := int64(unit), 0
	for m := n / unit; m >= unit; m /= unit {
		div *= unit
		exp++
	}
	return fmt.Sprintf("%.1f %cB", float64(n)/float64(div), "KMGTPE"[exp])
}
