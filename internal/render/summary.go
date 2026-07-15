package render

import (
	"fmt"
	"io"
	"time"

	"github.com/amberpixels/ele/internal/aggregator"
)

// Summary prints the once-only end-of-run report: plain text that survives
// scrollback and copy-paste. logPath and elapsed may be empty/zero.
func Summary(w io.Writer, s aggregator.Snapshot, logPath string, elapsed time.Duration) {
	fmt.Fprintln(w, outcome(s))
	if elapsed > 0 {
		fmt.Fprintf(w, "  time       %s\n", elapsed.Round(time.Millisecond))
	}

	fmt.Fprintln(w)
	fmt.Fprintf(w, "  %s\n", phaseBar("pre-data", s.Pre.Done, s.Pre.Total, ""))
	fmt.Fprintf(w, "  %s\n", phaseBar("data", s.Data.Done, s.Data.Total, dataNote(s)))
	fmt.Fprintf(w, "  %s\n", phaseBar("post-data", s.Post.Done, s.Post.Total, ""))

	fmt.Fprintf(w, "\n  errors     %s\n", errorCounter(s))
	for _, g := range s.Errors {
		fmt.Fprintf(w, "             %s\n", groupLine(g))
	}

	if len(s.Slowest) > 0 && s.Slowest[0].Dur >= time.Millisecond {
		fmt.Fprintln(w, "\n  slowest items")
		for _, it := range s.Slowest {
			fmt.Fprintf(w, "             %-8s %s %s\n", it.Dur.Round(time.Millisecond), it.Desc, it.Tag)
		}
	}

	if s.Unknown > 0 {
		fmt.Fprintf(w, "\n  unknown    %d line(s) - see the raw log\n", s.Unknown)
	}
	if logPath != "" {
		fmt.Fprintf(w, "\n  raw log    %s\n", logPath)
	}
}

// outcome is the headline verdict, driven by real (not benign) errors.
func outcome(s aggregator.Snapshot) string {
	switch {
	case s.ErrReal > 0:
		return fmt.Sprintf("completed with %d real error(s)", s.ErrReal)
	case s.ErrBenign > 0:
		return fmt.Sprintf("success - %d benign error(s) normalized to exit 0", s.ErrBenign)
	default:
		return "success"
	}
}
