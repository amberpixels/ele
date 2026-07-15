// Package render turns an aggregator.Snapshot into human output: a live,
// in-place-repainting block on a TTY, and a plain line-oriented form everywhere
// else. Frame and Summary are pure (snapshot -> lines) so they can be tested
// against fixed-width golden frames; the live driver adds only cursor motion.
package render

import (
	"fmt"
	"strings"

	"github.com/amberpixels/ele/internal/aggregator"
)

// barWidth is the fixed cell width of a phase bar.
const barWidth = 26

// maxGroups caps how many error groups the panel lists; the counter line always
// carries the full totals.
const maxGroups = 5

// Opts controls a rendered frame.
type Opts struct {
	Width   int    // terminal width; lines are truncated to it (0 = no limit)
	Title   string // e.g. "throwaway"; omitted when empty
	LogPath string // raw-log path shown in the panel; omitted when empty
	Spinner rune   // in-flight spinner glyph; 0 hides the spinner
}

// Frame renders the live status block as plain text lines (no ANSI). The live
// driver repaints exactly these lines; color is layered on separately so tests
// can assert on stable text.
func Frame(s aggregator.Snapshot, opt Opts) []string {
	var lines []string
	add := func(format string, a ...any) {
		lines = append(lines, truncate(fmt.Sprintf(format, a...), opt.Width))
	}

	if opt.Title != "" {
		add("Restoring %s", opt.Title)
		add("")
	}

	add("  %s", phaseBar("pre-data", s.Pre.Done, s.Pre.Total, ""))
	add("  %s", phaseBar("data", s.Data.Done, s.Data.Total, dataNote(s)))
	if fl := inFlightLine(s, opt.Spinner); fl != "" {
		add("             %s", fl)
	}
	add("  %s", phaseBar("post-data", s.Post.Done, s.Post.Total, ""))

	add("")
	add("  errors     %s", errorCounter(s))
	shown := s.Errors
	if len(shown) > maxGroups {
		shown = shown[:maxGroups]
	}
	for _, g := range shown {
		add("             %s", groupLine(g))
	}
	if opt.LogPath != "" {
		add("  log        %s", opt.LogPath)
	}
	return lines
}

// phaseBar renders "name  ███░░░  done/total  pct" with an optional trailing note.
func phaseBar(name string, done, total int, note string) string {
	filled := 0
	if total > 0 {
		filled = done * barWidth / total
	}
	bar := strings.Repeat("█", filled) + strings.Repeat("░", barWidth-filled)

	status := fmt.Sprintf("%d/%d", done, total)
	switch {
	case total > 0 && done >= total:
		status += "  done"
	case total > 0:
		status += fmt.Sprintf("  %d%%", done*100/total)
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

func errorCounter(s aggregator.Snapshot) string {
	return fmt.Sprintf("%d total · %d benign · %d real", s.ErrTotal, s.ErrBenign, s.ErrReal)
}

func groupLine(g aggregator.ErrorGroup) string {
	class := "real  "
	if g.Benign {
		class = "benign"
	}
	text := strings.TrimPrefix(g.Template, "could not execute query: ")
	line := fmt.Sprintf("%s  %s  ×%d", class, text, g.Count)
	if g.Distinct > 1 {
		line += fmt.Sprintf(" (%d objects)", g.Distinct)
	}
	return line
}

// truncate shortens s to at most width runes (ellipsis when cut). width <= 0
// means no limit.
func truncate(s string, width int) string {
	if width <= 0 {
		return s
	}
	r := []rune(s)
	if len(r) <= width {
		return s
	}
	if width == 1 {
		return "…"
	}
	return string(r[:width-1]) + "…"
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
