package engine

import (
	"bufio"
	"context"
	"fmt"
	"io"
	"os"
	"strconv"
	"sync"
	"time"

	"github.com/amberpixels/ele/internal/aggregator"
	"github.com/amberpixels/ele/internal/parser"
	"github.com/amberpixels/ele/internal/preflight"
	"github.com/amberpixels/ele/internal/render"
	"github.com/amberpixels/ele/internal/toc"
)

// replaySeconds is how long a live (TTY) replay is spread over so the block
// animates instead of flashing past. Override with ELE_REPLAY_SECONDS; 0 feeds
// instantly (the summary still prints). Non-TTY replays ignore pacing entirely.
const replaySeconds = 12

// Replay reruns a captured pg_restore stderr log through the full pipeline
// without touching a database - a safe dry-run of the live view. planSource is
// either a real dump (preflighted) or a saved `pg_restore -l` listing file;
// logPath is the captured stderr. On a TTY it drives the live block paced over
// replaySeconds, then prints the summary; otherwise it prints milestones and the
// summary. The exit code reflects the log's verdict: real errors -> 1, else 0.
func Replay(ctx context.Context, planSource, logPath string, stdout io.Writer, stderrFile *os.File) (int, error) {
	plan, err := loadPlan(ctx, planSource)
	if err != nil {
		return 1, err
	}

	lines, err := readLines(logPath)
	if err != nil {
		return 1, err
	}

	agg := aggregator.New(plan, aggregator.Config{Clean: true, NoOwner: true})
	p := parser.New()
	var mu sync.Mutex

	start := time.Now()
	live, stopTicker := startRenderer(stderrFile, agg, &mu, start, render.Opts{
		Width:   render.Width(stderrFile),
		Title:   "(replay) " + logPath,
		LogPath: logPath,
		Styles:  render.NewStyles(stderrFile),
	})
	if live != nil {
		defer live.Close()
	}

	pace := replayPacing(live != nil, len(lines))
	next := start
	for _, line := range lines {
		mu.Lock()
		for _, ev := range p.Feed(line) {
			agg.Feed(ev)
		}
		mu.Unlock()
		if pace > 0 {
			next = next.Add(pace)
			if d := time.Until(next); d > 2*time.Millisecond {
				time.Sleep(d)
			}
		}
	}

	stopTicker()
	mu.Lock()
	for _, ev := range p.Flush() {
		agg.Feed(ev)
	}
	agg.Finish() // the log is a completed run: mark phases done, remainder skipped
	snap := agg.Snapshot()
	mu.Unlock()

	if live != nil {
		live.Clear()
		live.Close()
	}
	// Show the replay's own elapsed as the total time: it's synthetic (paced),
	// but keeps the timer from vanishing when the run finishes.
	render.Summary(stderrFile, snap, logPath, time.Since(start), render.Width(stderrFile))

	if snap.ErrReal > 0 {
		return 1, nil
	}
	return 0, nil
}

// replayPacing returns the per-line delay that spreads a live replay over the
// configured window. Non-live (plain/non-TTY) replays and empty logs feed
// instantly.
func replayPacing(live bool, lines int) time.Duration {
	if !live || lines == 0 {
		return 0
	}
	secs := replaySeconds
	if v := os.Getenv("ELE_REPLAY_SECONDS"); v != "" {
		if n, err := strconv.Atoi(v); err == nil {
			secs = n
		}
	}
	if secs <= 0 {
		return 0
	}
	return time.Duration(secs) * time.Second / time.Duration(lines)
}

// loadPlan builds a RestorePlan from a dump or a saved `pg_restore -l` listing.
// preflight.Run handles the detection for both.
func loadPlan(ctx context.Context, source string) (*toc.RestorePlan, error) {
	res, err := preflight.Run(ctx, source)
	if err != nil {
		return nil, err
	}
	return res.Plan, nil
}

// readLines reads every line of a captured log, raising the scanner's line cap
// for long TOC tags the same way the rest of the pipeline does.
func readLines(path string) ([]string, error) {
	f, err := os.Open(path)
	if err != nil {
		return nil, err
	}
	defer f.Close()

	sc := bufio.NewScanner(f)
	sc.Buffer(make([]byte, 0, 64*1024), 4*1024*1024)
	var lines []string
	for sc.Scan() {
		lines = append(lines, sc.Text())
	}
	if err := sc.Err(); err != nil {
		return nil, fmt.Errorf("reading %s: %w", path, err)
	}
	return lines, nil
}
