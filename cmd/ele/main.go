// Command ele wraps pg_restore with a live, aggregated progress view.
//
// This build has no live wrapper wired in yet, so it exposes two offline modes:
//
//	ele <dump>                  preflight only: print the parsed restore plan
//	ele --replay <dump> <log>   replay a captured pg_restore stderr log through
//	                            the parser and aggregator and print the summary
//
// Neither mode connects to a database; --replay reads an already-captured log.
package main

import (
	"bufio"
	"context"
	"fmt"
	"io"
	"os"

	"github.com/amberpixels/ele/internal/aggregator"
	"github.com/amberpixels/ele/internal/parser"
	"github.com/amberpixels/ele/internal/preflight"
	"github.com/amberpixels/ele/internal/render"
)

func main() {
	if err := run(os.Args[1:], os.Stdout); err != nil {
		fmt.Fprintln(os.Stderr, "ele:", err)
		os.Exit(1)
	}
}

func run(args []string, out io.Writer) error {
	switch {
	case len(args) == 3 && args[0] == "--replay":
		return replay(out, args[1], args[2])
	case len(args) == 1 && args[0] != "-h" && args[0] != "--help":
		return preflightOnly(out, args[0])
	default:
		return fmt.Errorf("usage:\n" +
			"  ele <dump>                  print the parsed restore plan\n" +
			"  ele --replay <dump> <log>   replay a captured stderr log and print the summary")
	}
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
// the parser and aggregator - the full pipeline minus the live renderer - and
// prints the summary. It touches no database.
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

	// The captured logs were produced with --clean --no-owner; classify against
	// that. A live run will read these from the child's argv.
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
