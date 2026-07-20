// Package render turns an aggregator.Snapshot into human output: a live,
// in-place-repainting block on a TTY, and a plain line-oriented form everywhere
// else. Frame and Summary carry the layout; color comes from lipgloss Styles
// that degrade to plain on non-TTY / NO_COLOR output.
package render

import (
	"fmt"
	"strings"
	"time"

	"github.com/amberpixels/ele/internal/aggregator"
	"github.com/amberpixels/ele/internal/humanize"
	"github.com/amberpixels/years"
	"github.com/charmbracelet/x/ansi"
)

// barWidth is the fixed cell width of a phase bar.
const barWidth = 26

// labelCol is the column where a labeled row's content begins: 2 spaces of
// indent + a 10-cell label + 1 space, matching phaseBar's "%-10s %s" layout.
const labelCol = 13

// metaWidth is the column that right-aligned meta (the elapsed clock and the
// current-item timer) aligns to. Anchoring to a fixed column keeps those numbers
// just past the phase status instead of drifting to the far edge of a wide
// terminal; narrower terminals fall back to their actual width.
const metaWidth = 74

// workingTimerMin is the minimum age before the current item shows a timer -
// quick items would just flicker 0:00.
const workingTimerMin = 10 * time.Second

// metaEdge is the right-alignment column for meta, capped to the real width.
func metaEdge(width int) int {
	if width > 0 && width < metaWidth {
		return width
	}
	return metaWidth
}

// Each phase fills with a distinct shade so three stacked bars read as three
// separate bars rather than one merged block; the empty track is uniform.
const (
	fillPre   = '█'
	fillData  = '▓'
	fillPost  = '▒'
	fillEmpty = '░'
)

// maxGroups caps the live panel; summaryMaxGroups caps the taller final summary.
// Both note anything hidden, and the counter line always carries the full totals.
// Because Errors is sorted real-first then most-recent-first, the groups folded
// away are always the most benign and oldest - every real error stays visible.
const (
	maxGroups        = 5
	summaryMaxGroups = 10
)

// Opts controls a rendered frame.
type Opts struct {
	Width   int           // terminal width; lines are truncated to it (0 = no limit)
	Title   string        // e.g. "throwaway"; omitted when empty
	LogPath string        // raw-log path shown in the panel; omitted when empty
	Spinner rune          // in-flight spinner glyph; 0 hides the spinner
	Elapsed time.Duration // wall time since the restore started; 0 hides it
	Styles  *Styles       // color styles; nil = plain
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
		add("%s", headerLine(st, "Restoring "+opt.Title, opt.Elapsed, opt.Width))
		add("")
	}

	if cl := cleanupLine(st, s, opt.Spinner); cl != "" {
		add("  %-10s %s", "cleanup", cl)
	}
	add("  %s", phaseBar(st, "pre-data", fillPre, s.Pre, ""))
	add("  %s", phaseBar(st, "data", fillData, s.Data, dataNote(s)))
	add("  %s", phaseBar(st, "post-data", fillPost, s.Post, ""))
	// The working line sits below all three bars: under -j, data and post-data
	// items run concurrently, so it reflects current activity across the restore.
	// Its spinner and live timer are what tell a long single item apart from a hang.
	if wl := workingLine(st, s.Working, opt.Spinner, opt.Width); wl != "" {
		add("  %-10s %s", "working", wl)
	}

	add("")
	// The log line sits above the error panel: the panel grows as groups appear,
	// so a trailing log line would keep sliding down on every repaint.
	if opt.LogPath != "" {
		add("  log        %s", st.dim.Render(opt.LogPath))
	}
	add("  errors     %s", errorCounter(st, s))
	shown, hidden := capGroups(s.Errors, maxGroups)
	for _, g := range shown {
		add("             %s", groupLine(st, g))
	}
	if hidden > 0 {
		add("             %s", st.dim.Render(moreGroupsNote(hidden)))
	}
	return lines
}

// phaseBar renders "name  ▓▓▓░░░  done/total  pct" with an optional trailing
// note. fill is the phase's filled-cell glyph; the empty track is uniform. A
// complete phase reads 100% and "done" even if pg_restore skipped some entries -
// those show as a "N skipped" note, and the fraction still shows what succeeded.
func phaseBar(st *Styles, name string, fill rune, p aggregator.PhaseProgress, note string) string {
	done, total := p.Done, p.Total
	complete := p.Complete || (total > 0 && done >= total)
	filled := 0
	switch {
	case complete:
		filled = barWidth
	case total > 0:
		filled = done * barWidth / total
	}
	bar := st.bar.Render(strings.Repeat(string(fill), filled)) + st.empty.Render(strings.Repeat(string(fillEmpty), barWidth-filled))

	status := fmt.Sprintf("%d/%d", done, total)
	switch {
	case complete:
		status += "  " + st.done.Render("done")
	case total > 0:
		status += "  " + st.pct.Render(fmt.Sprintf("%d%%", done*100/total))
	}

	notes := make([]string, 0, 2)
	if note != "" {
		notes = append(notes, note)
	}
	if p.Skipped > 0 {
		notes = append(notes, st.skipped.Render(fmt.Sprintf("%d skipped", p.Skipped)))
	}
	line := fmt.Sprintf("%-10s %s  %s", name, bar, status)
	if len(notes) > 0 {
		line += "  " + strings.Join(notes, "  ")
	}
	return line
}

// cleanupLine summarizes the --clean DROP wave, rendered above the section bars.
// While it runs: a spinner and a live drop count; once it's over: the same row
// with the spinner gone and the count frozen. It persists from the first drop
// onward (never hidden), and its count is padded to the bar width so it lines up
// with the phase rows' N/M status below.
func cleanupLine(st *Styles, s aggregator.Snapshot, spinner rune) string {
	if s.DropCount == 0 {
		return ""
	}
	label, prefix := "dropped old objects", ""
	if s.Dropping {
		label = "dropping old objects"
		if spinner != 0 {
			prefix = string(spinner) + " "
		}
	}
	activity := prefix + st.dim.Render(label)
	pad := barWidth - ansi.StringWidth(activity)
	if pad < 1 {
		pad = 1
	}
	return activity + strings.Repeat(" ", pad) + fmt.Sprintf("  %d dropped", s.DropCount)
}

// dataNote adds a byte readout to the data bar when preflight sized the dump.
func dataNote(s aggregator.Snapshot) string {
	if !s.ByteSized {
		return ""
	}
	return humanize.Bytes(s.BytesDone) + " / " + humanize.Bytes(s.BytesTotal)
}

// headerLine renders the title with the elapsed time right-aligned and dim.
// When width is unknown it falls back to appending the time after a separator;
// elapsed <= 0 is omitted entirely.
func headerLine(st *Styles, title string, elapsed time.Duration, width int) string {
	if elapsed <= 0 {
		return title
	}
	right := st.timer.Render(years.FormatDurationClock(elapsed))
	edge := metaEdge(width)
	if edge > ansi.StringWidth(title)+ansi.StringWidth(right)+1 {
		gap := edge - ansi.StringWidth(title) - ansi.StringWidth(right)
		return title + strings.Repeat(" ", gap) + right
	}
	return title + "  " + right
}

// workingLine renders the object currently being restored: a spinner (proof of
// life), its type and name, and its own elapsed timer right-aligned. Under -j
// the first item is the longest-running and "· +N more" notes the rest. The
// timer is protected from truncation: a long name is shortened, never the time.
func workingLine(st *Styles, items []aggregator.WorkItem, spinner rune, width int) string {
	if len(items) == 0 {
		return ""
	}
	it := items[0]

	var left strings.Builder
	if spinner != 0 {
		left.WriteString(string(spinner) + " ")
	}
	if it.Desc != "" {
		left.WriteString(st.dim.Render(it.Desc) + " ")
	}
	left.WriteString(it.Name)
	if extra := len(items) - 1; extra > 0 {
		left.WriteString(st.dim.Render(fmt.Sprintf(" · +%d more", extra)))
	}
	ls := left.String()

	// Quick items show no timer; only ones that linger get one, anchored to the
	// meta column so it lines up with the header clock rather than the far edge.
	if it.Elapsed < workingTimerMin {
		return ls
	}
	right := st.timer.Render(years.FormatDurationClock(it.Elapsed))

	avail := metaEdge(width) - labelCol
	if avail <= 0 {
		return ls + "  " + right
	}
	rw := ansi.StringWidth(right)
	if gap := avail - ansi.StringWidth(ls) - rw; gap >= 1 {
		return ls + strings.Repeat(" ", gap) + right
	}
	if keep := avail - rw - 1; keep >= 1 {
		return ansi.Truncate(ls, keep, "…") + " " + right
	}
	return ansi.Truncate(ls, avail, "…") // too narrow for both; keep the name
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

// capGroups limits how many error groups a panel lists, returning the visible
// slice and how many were folded away. Errors arrives sorted real-first then
// most-recent-first, so the hidden tail is always benign and oldest - and every
// real group stays visible even when that means keeping more than limit.
func capGroups(errs []aggregator.ErrorGroup, limit int) (shown []aggregator.ErrorGroup, hidden int) {
	if len(errs) <= limit {
		return errs, 0
	}
	keep := limit
	real := 0
	for _, g := range errs {
		if !g.Benign {
			real++
		}
	}
	if real > keep {
		keep = real // never hide a real error group
	}
	if keep >= len(errs) {
		return errs, 0
	}
	return errs[:keep], len(errs) - keep
}

// moreGroupsNote labels the benign groups a panel folded away to save space.
func moreGroupsNote(hidden int) string {
	if hidden == 1 {
		return "… 1 more benign group"
	}
	return fmt.Sprintf("… %d more benign groups", hidden)
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
