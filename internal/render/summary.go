package render

import (
	"fmt"
	"io"
	"strings"
	"time"

	"github.com/amberpixels/ele/internal/aggregator"
	"github.com/amberpixels/years"
	"github.com/charmbracelet/x/ansi"
)

// Summary prints the once-only end-of-run report: plain text that survives
// scrollback and copy-paste, colored when w is a capable terminal. It mirrors the
// live frame's layout so the block appears to freeze in place rather than jump -
// the elapsed clock stays top-right on the outcome line, and the cleanup row
// keeps its spot above the bars. logPath/elapsed may be empty/zero; width is the
// terminal width for right-aligning the clock (0 = default).
func Summary(w io.Writer, s aggregator.Snapshot, logPath string, elapsed time.Duration, width int) {
	st := NewStyles(w)

	fmt.Fprintln(w, outcomeLine(st, s, elapsed, width))

	fmt.Fprintln(w)
	if cl := cleanupLine(st, s, 0); cl != "" {
		fmt.Fprintf(w, "  %-10s %s\n", "cleanup", cl)
	}
	fmt.Fprintf(w, "  %s\n", phaseBar(st, "pre-data", fillPre, s.Pre, ""))
	fmt.Fprintf(w, "  %s\n", phaseBar(st, "data", fillData, s.Data, dataNote(s)))
	fmt.Fprintf(w, "  %s\n", phaseBar(st, "post-data", fillPost, s.Post, ""))

	if logPath != "" {
		fmt.Fprintf(w, "\n  raw log    %s\n", st.dim.Render(logPath))
	}

	fmt.Fprintf(w, "\n  errors     %s\n", errorCounter(st, s))
	shown, hidden := capGroups(s.Errors, summaryMaxGroups)
	for _, g := range shown {
		fmt.Fprintf(w, "             %s\n", groupLine(st, g))
	}
	if hidden > 0 {
		fmt.Fprintf(w, "             %s\n", st.dim.Render(moreGroupsNote(hidden)))
	}

	if len(s.Slowest) > 0 && s.Slowest[0].Dur >= time.Millisecond {
		fmt.Fprintln(w, "\n  slowest items")
		for _, it := range s.Slowest {
			fmt.Fprintf(w, "             %s\n", st.dim.Render(
				fmt.Sprintf("%-8s %s %s", it.Dur.Round(time.Millisecond), it.Desc, it.Tag)))
		}
	}

	if s.Unknown > 0 {
		fmt.Fprintf(w, "\n  unknown    %d line(s) - see the raw log\n", s.Unknown)
	}

	// The skipped-objects explanation sits at the very bottom: it's a full-width
	// sentence, so keeping it out of the header means the phase bars stay at the
	// same height they were during the live run instead of being pushed down.
	if skipped := s.Pre.Skipped + s.Data.Skipped + s.Post.Skipped; skipped > 0 {
		fmt.Fprintf(w, "\n  %s\n", st.skipped.Render(fmt.Sprintf(
			"%d planned object(s) were not restored - pg_restore skipped them "+
				"(usually a dependency cascade); see the raw log", skipped)))
	}
}

// outcomeLine is the summary's header: the verdict with the final elapsed clock
// right-aligned in the same spot the live header kept it, so the timer looks like
// it simply stopped ticking rather than moving into a new row.
func outcomeLine(st *Styles, s aggregator.Snapshot, elapsed time.Duration, width int) string {
	line := outcome(st, s)
	if elapsed <= 0 {
		return line
	}
	right := st.timer.Render(years.FormatDurationClock(elapsed))
	edge := metaEdge(width)
	if edge > ansi.StringWidth(line)+ansi.StringWidth(right)+1 {
		gap := edge - ansi.StringWidth(line) - ansi.StringWidth(right)
		return line + strings.Repeat(" ", gap) + right
	}
	return line + "  " + right
}

// outcome is the headline verdict, driven by real (not benign) errors.
func outcome(st *Styles, s aggregator.Snapshot) string {
	switch {
	case s.ErrReal > 0:
		return st.fail.Render(fmt.Sprintf("completed with %d real error(s)", s.ErrReal))
	case s.ErrBenign > 0:
		return st.success.Render(fmt.Sprintf("success - %d benign error(s) normalized to exit 0", s.ErrBenign))
	default:
		return st.success.Render("success")
	}
}
