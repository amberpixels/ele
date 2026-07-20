// Package engine wires the pipeline into the live `ele <pg_restore args>`
// wrapper: preflight the dump, run pg_restore through the runner, feed its
// stderr to the parser and aggregator, and repaint a status block on a ticker.
// On exit it clears the block, prints the summary, and normalizes the exit code
// when every error was benign.
package engine

import (
	"context"
	"errors"
	"fmt"
	"io"
	"os"
	"os/exec"
	"sync"
	"time"

	"github.com/amberpixels/ele/internal/aggregator"
	"github.com/amberpixels/ele/internal/parser"
	"github.com/amberpixels/ele/internal/preflight"
	"github.com/amberpixels/ele/internal/render"
	"github.com/amberpixels/ele/internal/runner"
	"github.com/amberpixels/ele/internal/toc"
)

// tickInterval is the live repaint cadence (~10 fps); plainInterval is the
// slower cadence for checking plain-mode progress milestones.
const (
	tickInterval  = 100 * time.Millisecond
	plainInterval = 500 * time.Millisecond
)

var spinner = []rune("⣾⣽⣻⢿⡿⣟⣯⣷")

// Run executes `pg_restore args` as a live-wrapped restore and returns the exit
// code ele should exit with. stdout receives the child's stdout untouched;
// stderrFile is the terminal channel for the status block and summary.
func Run(ctx context.Context, args []string, stdout io.Writer, stderrFile *os.File) (int, error) {
	f := detectFlags(args)

	// Not a restore-to-database (no -d): ele adds nothing, so pass through.
	if !f.hasDB {
		return passthrough(ctx, args, stdout, stderrFile)
	}

	dumpPath := findDumpPath(args, func(p string) bool {
		_, err := os.Stat(p)
		return err == nil
	})
	plan := preflightPlan(ctx, dumpPath, stderrFile)
	if plan == nil {
		// No plan means no denominators; fall back to a transparent run rather
		// than draw meaningless bars.
		fmt.Fprintln(stderrFile, "ele: no restore plan available; passing through")
		return passthrough(ctx, args, stdout, stderrFile)
	}

	logPath := "ele-" + time.Now().Format("20060102-150405") + ".log"
	logf, err := os.Create(logPath)
	if err != nil {
		return 1, fmt.Errorf("creating log: %w", err)
	}
	defer logf.Close()

	agg := aggregator.New(plan, aggregator.Config{Clean: f.clean, NoOwner: f.noOwner})
	p := parser.New()
	var mu sync.Mutex

	start := time.Now()
	live, stopTicker := startRenderer(stderrFile, agg, &mu, start, render.Opts{
		Width:   render.Width(stderrFile),
		Title:   f.dbName,
		LogPath: logPath,
		Styles:  render.NewStyles(stderrFile),
	})
	if live != nil {
		defer live.Close() // restore the cursor even on panic; idempotent
	}

	result, runErr := runner.Run(ctx, runner.Options{
		Args:   args,
		Stdout: stdout,
		RawLog: logf,
		OnLine: func(line string) {
			mu.Lock()
			for _, ev := range p.Feed(line) {
				agg.Feed(ev)
			}
			mu.Unlock()
		},
	})
	elapsed := time.Since(start)

	stopTicker()
	mu.Lock()
	for _, ev := range p.Flush() {
		agg.Feed(ev)
	}
	// A normal exit (even a failing code) means pg_restore processed every item
	// it was going to; the phase remainder is skipped, not pending. A signal kill
	// leaves the rest genuinely incomplete, so don't claim the phases finished.
	if result != nil && !result.Signaled {
		agg.Finish()
	}
	snap := agg.Snapshot()
	mu.Unlock()

	if live != nil {
		live.Clear()
		live.Close()
	}
	if runErr != nil {
		fmt.Fprintf(stderrFile, "ele: %v\n", runErr)
	}

	render.Summary(stderrFile, snap, logPath, elapsed, render.Width(stderrFile))
	return exitCode(result, snap, stderrFile), nil
}

// startRenderer starts the progress loop: a live repaint block on a TTY, or
// periodic one-line milestones when output is plain. It returns the Live writer
// (nil in plain mode) and a stop function that halts the ticker.
func startRenderer(stderrFile *os.File, agg *aggregator.Aggregator, mu *sync.Mutex, start time.Time, opt render.Opts) (*render.Live, func()) {
	done := make(chan struct{})
	var wg sync.WaitGroup

	if render.Plain(stderrFile) {
		pp := render.NewPlainProgress(stderrFile)
		wg.Go(func() {
			t := time.NewTicker(plainInterval)
			defer t.Stop()
			for {
				select {
				case <-done:
					return
				case <-t.C:
					mu.Lock()
					snap := agg.Snapshot()
					mu.Unlock()
					pp.Update(snap, time.Since(start))
				}
			}
		})
		return nil, func() { close(done); wg.Wait() }
	}

	live := render.NewLive(stderrFile)
	wg.Go(func() {
		t := time.NewTicker(tickInterval)
		defer t.Stop()
		for i := 0; ; i++ {
			select {
			case <-done:
				return
			case <-t.C:
				mu.Lock()
				snap := agg.Snapshot()
				mu.Unlock()
				opt.Spinner = spinner[i%len(spinner)]
				opt.Elapsed = time.Since(start)
				live.Render(render.Frame(snap, opt))
			}
		}
	})
	return live, func() { close(done); wg.Wait() }
}

// preflightPlan runs preflight, returning nil (with a note) when it can't.
func preflightPlan(ctx context.Context, dumpPath string, stderrFile *os.File) *toc.RestorePlan {
	if dumpPath == "" {
		return nil
	}
	res, err := preflight.Run(ctx, dumpPath)
	if err != nil {
		fmt.Fprintf(stderrFile, "ele: preflight failed: %v\n", err)
		return nil
	}
	return res.Plan
}

// exitCode applies benign normalization: a nonzero child exit whose errors were
// all benign becomes 0 (unless ELE_STRICT_EXIT is set), replacing the `|| true`
// hack. A signal death or any real error keeps a failing code.
func exitCode(result *runner.Result, snap aggregator.Snapshot, stderrFile *os.File) int {
	code := result.ExitCode
	if code != 0 && !result.Signaled && snap.ErrReal == 0 && os.Getenv("ELE_STRICT_EXIT") == "" {
		fmt.Fprintf(stderrFile, "\nele: pg_restore exited %d but all errors were benign; normalizing to 0\n", code)
		return 0
	}
	return code
}

// passthrough runs pg_restore with the args verbatim, inheriting stdio - ele
// adds nothing here.
func passthrough(ctx context.Context, args []string, stdout io.Writer, stderrFile *os.File) (int, error) {
	cmd := exec.CommandContext(ctx, "pg_restore", args...)
	cmd.Stdin = os.Stdin
	cmd.Stdout = stdout
	cmd.Stderr = stderrFile
	err := cmd.Run()
	if err == nil {
		return 0, nil
	}
	if ee, ok := errors.AsType[*exec.ExitError](err); ok {
		return ee.ExitCode(), nil
	}
	return 1, err
}
