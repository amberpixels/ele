package render

import (
	"fmt"
	"io"
	"time"

	"github.com/amberpixels/ele/internal/aggregator"
)

// Summary prints the once-only end-of-run report: plain text that survives
// scrollback and copy-paste, colored when w is a capable terminal. logPath and
// elapsed may be empty/zero.
func Summary(w io.Writer, s aggregator.Snapshot, logPath string, elapsed time.Duration) {
	st := NewStyles(w)

	fmt.Fprintln(w, outcome(st, s))
	if elapsed > 0 {
		fmt.Fprintf(w, "  time       %s\n", elapsed.Round(time.Millisecond))
	}

	fmt.Fprintln(w)
	fmt.Fprintf(w, "  %s\n", phaseBar(st, "pre-data", fillPre, s.Pre.Done, s.Pre.Total, ""))
	fmt.Fprintf(w, "  %s\n", phaseBar(st, "data", fillData, s.Data.Done, s.Data.Total, dataNote(s)))
	fmt.Fprintf(w, "  %s\n", phaseBar(st, "post-data", fillPost, s.Post.Done, s.Post.Total, ""))

	fmt.Fprintf(w, "\n  errors     %s\n", errorCounter(st, s))
	for _, g := range s.Errors {
		fmt.Fprintf(w, "             %s\n", groupLine(st, g))
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
	if logPath != "" {
		fmt.Fprintf(w, "\n  raw log    %s\n", st.dim.Render(logPath))
	}
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
