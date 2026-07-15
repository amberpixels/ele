// Package render turns an aggregator.Snapshot into human output: a live,
// in-place-repainting block on a TTY, and a plain line-oriented form everywhere
// else. Frame and Summary carry the layout; color comes from lipgloss Styles
// that degrade to plain on non-TTY / NO_COLOR output.
package render

import (
	"fmt"
	"strings"

	"github.com/amberpixels/ele/internal/aggregator"
	"github.com/charmbracelet/x/ansi"
)

// barWidth is the fixed cell width of a phase bar.
const barWidth = 26

// maxGroups caps how many error groups the panel lists; the counter line always
// carries the full totals.
const maxGroups = 5

// Opts controls a rendered frame.
type Opts struct {
	Width   int     // terminal width; lines are truncated to it (0 = no limit)
	Title   string  // e.g. "throwaway"; omitted when empty
	LogPath string  // raw-log path shown in the panel; omitted when empty
	Spinner rune    // in-flight spinner glyph; 0 hides the spinner
	Styles  *Styles // color styles; nil = plain
}

// Frame renders the live status block. Lines are colored per Opts.Styles and
// truncated (ANSI-aware) to Opts.Width.
func Frame(s aggregator.Snapshot, opt Opts) []string {
	st := opt.Styles
	if st == nil {
		st = plainStyles()
	}

	var lines []string
	add := func(format string, a ...any) {
		lines = append(lines, truncateLine(fmt.Sprintf(format, a...), opt.Width))
	}

	if opt.Title != "" {
		add("Restoring %s", opt.Title)
		add("")
	}

	add("  %s", phaseBar(st, "pre-data", s.Pre.Done, s.Pre.Total, ""))
	add("  %s", phaseBar(st, "data", s.Data.Done, s.Data.Total, dataNote(s)))
	if fl := inFlightLine(s, opt.Spinner); fl != "" {
		add("             %s", fl)
	}
	add("  %s", phaseBar(st, "post-data", s.Post.Done, s.Post.Total, ""))

	add("")
	add("  errors     %s", errorCounter(st, s))
	shown := s.Errors
	if len(shown) > maxGroups {
		shown = shown[:maxGroups]
	}
	for _, g := range shown {
		add("             %s", groupLine(st, g))
	}
	if opt.LogPath != "" {
		add("  log        %s", st.dim.Render(opt.LogPath))
	}
	return lines
}

// phaseBar renders "name  ███░░░  done/total  pct" with an optional trailing note.
func phaseBar(st *Styles, name string, done, total int, note string) string {
	filled := 0
	if total > 0 {
		filled = done * barWidth / total
	}
	bar := st.bar.Render(strings.Repeat("█", filled)) + st.empty.Render(strings.Repeat("░", barWidth-filled))

	status := fmt.Sprintf("%d/%d", done, total)
	switch {
	case total > 0 && done >= total:
		status += "  " + st.done.Render("done")
	case total > 0:
		status += "  " + st.pct.Render(fmt.Sprintf("%d%%", done*100/total))
	}
	line := fmt.Sprintf("%-10s %s  %s", name, bar, status)
	if note != "" {
		line += "  " + note
	}
	return line
}

// dataNote adds a byte readout to the data bar when preflight sized the dump.
func dataNote(s aggregator.Snapshot) string {
	if !s.ByteSized {
		return ""
	}
	return humanBytes(s.BytesDone) + " / " + humanBytes(s.BytesTotal)
}

// inFlightLine summarizes the currently-restoring parallel items, longest first.
func inFlightLine(s aggregator.Snapshot, spinner rune) string {
	if len(s.InFlight) == 0 {
		return ""
	}
	const showN = 3
	var names []string
	for i, it := range s.InFlight {
		if i >= showN {
			break
		}
		names = append(names, it.Tag)
	}
	line := strings.Join(names, " · ")
	if extra := len(s.InFlight) - showN; extra > 0 {
		line += fmt.Sprintf(" · +%d in flight", extra)
	}
	if spinner != 0 {
		line = string(spinner) + " " + line
	}
	return line
}

// errorCounter renders "N total · N benign · N real", the real count in red
// when nonzero.
func errorCounter(st *Styles, s aggregator.Snapshot) string {
	benign := st.benign.Render(fmt.Sprintf("%d benign", s.ErrBenign))
	realStyle := st.dim
	if s.ErrReal > 0 {
		realStyle = st.real
	}
	real := realStyle.Render(fmt.Sprintf("%d real", s.ErrReal))
	return fmt.Sprintf("%d total · %s · %s", s.ErrTotal, benign, real)
}

// groupLine renders one error group, dim for benign and red for real.
func groupLine(st *Styles, g aggregator.ErrorGroup) string {
	text := strings.TrimPrefix(g.Template, "could not execute query: ")
	line := fmt.Sprintf("%-6s  %s  ×%d", class(g), text, g.Count)
	if g.Distinct > 1 {
		line += fmt.Sprintf(" (%d objects)", g.Distinct)
	}
	if g.Benign {
		return st.benign.Render(line)
	}
	return st.real.Render(line)
}

func class(g aggregator.ErrorGroup) string {
	if g.Benign {
		return "benign"
	}
	return "real"
}

// truncateLine shortens a possibly-styled line to width visible cells, keeping
// ANSI sequences intact. width <= 0 means no limit.
func truncateLine(s string, width int) string {
	if width <= 0 {
		return s
	}
	return ansi.Truncate(s, width, "…")
}

// humanBytes renders a byte count compactly, e.g. "3.1 GB".
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
