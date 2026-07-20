package render

import (
	"bytes"
	"strings"
	"testing"
	"time"

	"github.com/amberpixels/ele/internal/aggregator"
)

// sample mirrors the prod-scale snapshot: pre-data done, data mid-flight,
// post-data barely started, a big benign error storm.
func sample() aggregator.Snapshot {
	return aggregator.Snapshot{
		Pre:      aggregator.PhaseProgress{Done: 1009, Total: 1009},
		Data:     aggregator.PhaseProgress{Done: 166, Total: 503},
		Post:     aggregator.PhaseProgress{Done: 4, Total: 1156},
		ErrTotal: 1909, ErrBenign: 1909, ErrReal: 0,
		Errors: []aggregator.ErrorGroup{
			{Template: `could not execute query: ERROR:  index "…" does not exist`, Count: 746, Distinct: 746, Benign: true},
			{Template: `could not execute query: ERROR:  relation "…" does not exist`, Count: 654, Distinct: 252, Benign: true},
		},
	}
}

func joined(lines []string) string { return strings.Join(lines, "\n") }

func TestFrameContent(t *testing.T) {
	out := joined(Frame(sample(), Opts{Title: "throwaway", LogPath: "run.log"}))
	t.Log("\n" + out)

	wants := []string{
		"Restoring throwaway",
		"pre-data",
		"1009/1009  done",
		"166/503  33%", // data percentage
		"post-data",
		"1909 total · 1909 benign · 0 real",
		`benign  ERROR:  index "…" does not exist  ×746 (746 objects)`,
		"log        run.log",
	}
	for _, w := range wants {
		if !strings.Contains(out, w) {
			t.Errorf("frame missing %q", w)
		}
	}
}

func TestFrameDataBytes(t *testing.T) {
	s := sample()
	s.ByteSized = true
	s.BytesDone = 3 * 1024 * 1024 * 1024
	s.BytesTotal = 5 * 1024 * 1024 * 1024
	out := joined(Frame(s, Opts{}))
	if !strings.Contains(out, "3.0 GB / 5.0 GB") {
		t.Errorf("data byte note missing:\n%s", out)
	}
}

func TestFrameWorking(t *testing.T) {
	s := sample()
	s.Working = []aggregator.WorkItem{
		{Desc: "TABLE DATA", Name: "big_table", Elapsed: 72 * time.Second},
		{Name: "other"}, {Name: "third"}, {Name: "fourth"},
	}
	out := joined(Frame(s, Opts{Spinner: '⣾'}))
	if !strings.Contains(out, "working") || !strings.Contains(out, "big_table") {
		t.Errorf("working line missing object:\n%s", out)
	}
	if !strings.Contains(out, "+3 more") {
		t.Errorf("working line missing overflow count:\n%s", out)
	}
	if !strings.Contains(out, "1:12") { // 72s -> M:SS
		t.Errorf("working line missing item timer:\n%s", out)
	}
}

// TestFrameCleanupAboveBars: while the DROP wave runs, the cleanup row renders
// just above the section bars.
func TestFrameCleanupAboveBars(t *testing.T) {
	s := aggregator.Snapshot{Pre: aggregator.PhaseProgress{Total: 1017}, DropCount: 1439, Dropping: true}
	frame := Frame(s, Opts{Width: 100, Title: "db", Spinner: '⠧', LogPath: "run.log"})
	ci, pi := -1, -1
	for i, l := range frame {
		if strings.HasPrefix(l, "  cleanup") {
			ci = i
		}
		if strings.HasPrefix(l, "  pre-data") {
			pi = i
		}
	}
	if ci < 0 || pi < 0 || ci >= pi {
		t.Errorf("cleanup should render just above pre-data (cleanup=%d pre-data=%d):\n%s", ci, pi, strings.Join(frame, "\n"))
	}
}

// TestSummarySkippedNoteAtBottom: the full-width skipped explanation goes at the
// very bottom, after the bars and errors, so it doesn't push the bars down.
func TestSummarySkippedNoteAtBottom(t *testing.T) {
	var buf bytes.Buffer
	s := sample()
	s.Pre = aggregator.PhaseProgress{Done: 1009, Total: 1017, Complete: true, Skipped: 8}
	Summary(&buf, s, "run.log", 0, 100)
	out := buf.String()
	note := strings.Index(out, "planned object(s) were not restored")
	bars := strings.Index(out, "pre-data")
	errs := strings.Index(out, "errors ")
	if note < 0 {
		t.Fatal("skipped note missing")
	}
	if note < bars || note < errs {
		t.Errorf("skipped note should be at the bottom (note=%d bars=%d errors=%d)", note, bars, errs)
	}
}

func TestFrameHeaderElapsed(t *testing.T) {
	out := joined(Frame(sample(), Opts{Title: "db", Elapsed: 4*time.Minute + 12*time.Second}))
	if !strings.Contains(out, "Restoring db") || !strings.Contains(out, "4:12") {
		t.Errorf("header elapsed missing:\n%s", out)
	}
}

func TestFrameGroupCap(t *testing.T) {
	s := sample()
	// Seven groups, but only maxGroups (5) are listed, with a folded-count note.
	s.Errors = nil
	for range 7 {
		s.Errors = append(s.Errors, aggregator.ErrorGroup{Template: "e", Count: 1, Benign: true})
	}
	out := joined(Frame(s, Opts{}))
	if shown := strings.Count(out, "benign  e  ×1"); shown != maxGroups {
		t.Errorf("listed %d groups, want %d", shown, maxGroups)
	}
	if !strings.Contains(out, "2 more benign groups") { // 7 - 5 hidden
		t.Errorf("missing folded-count note:\n%s", out)
	}
}

// TestCapGroupsKeepsRealErrors: a real group must never be folded away, even
// when the benign groups alone would fill the limit.
func TestCapGroupsKeepsRealErrors(t *testing.T) {
	var errs []aggregator.ErrorGroup
	errs = append(errs, aggregator.ErrorGroup{Template: "boom", Count: 1}) // real, sorted first
	for range 12 {
		errs = append(errs, aggregator.ErrorGroup{Template: "noise", Count: 1, Benign: true})
	}
	shown, hidden := capGroups(errs, 5)
	if hidden == 0 {
		t.Fatal("expected some benign groups to be hidden")
	}
	sawReal := false
	for _, g := range shown {
		if !g.Benign {
			sawReal = true
		}
	}
	if !sawReal {
		t.Error("real group was hidden by the cap")
	}
}

func TestFrameTruncatesToWidth(t *testing.T) {
	const w = 30
	for _, l := range Frame(sample(), Opts{Width: w, Title: "throwaway", LogPath: "some/long/path/run.log"}) {
		if n := len([]rune(l)); n > w {
			t.Errorf("line exceeds width %d (%d runes): %q", w, n, l)
		}
	}
}

func TestSummaryOutcome(t *testing.T) {
	var buf bytes.Buffer
	Summary(&buf, sample(), "run.log", 0, 100)
	out := buf.String()
	if !strings.Contains(out, "success - 1909 benign error(s) normalized to exit 0") {
		t.Errorf("outcome wrong:\n%s", out)
	}

	buf.Reset()
	s := sample()
	s.ErrReal = 2
	Summary(&buf, s, "", 0, 100)
	if !strings.Contains(buf.String(), "completed with 2 real error(s)") {
		t.Errorf("real-error outcome wrong:\n%s", buf.String())
	}
}

func TestPlainProgress(t *testing.T) {
	var buf bytes.Buffer
	pp := NewPlainProgress(&buf)

	snap := func(pre, data, post int) aggregator.Snapshot {
		return aggregator.Snapshot{
			Pre:  aggregator.PhaseProgress{Done: pre, Total: 100},
			Data: aggregator.PhaseProgress{Done: data, Total: 100},
			Post: aggregator.PhaseProgress{Done: post, Total: 100},
		}
	}

	pp.Update(snap(0, 0, 0), 0)    // first tick -> emits
	pp.Update(snap(50, 0, 0), 0)   // no milestone (pre incomplete, deciles unchanged)
	pp.Update(snap(100, 0, 0), 0)  // pre completes -> emits
	pp.Update(snap(100, 10, 0), 0) // data crosses 10% -> emits
	pp.Update(snap(100, 15, 0), 0) // same decile -> silent
	pp.Update(snap(100, 20, 0), 0) // data crosses 20% -> emits

	out := buf.String()
	if n := strings.Count(out, "\n"); n != 4 {
		t.Errorf("emitted %d lines, want 4:\n%s", n, out)
	}
	if !strings.Contains(out, "pre 100/100") || !strings.Contains(out, "data 20/100 20%") {
		t.Errorf("last line wrong:\n%s", out)
	}
}

func TestLiveRepaint(t *testing.T) {
	var buf bytes.Buffer
	l := NewLive(&buf)
	l.Render([]string{"line-a", "line-b"})
	l.Render([]string{"line-c", "line-d"})

	out := buf.String()
	if !strings.Contains(out, ansiHideCursor) {
		t.Error("first render should hide the cursor")
	}
	if !strings.Contains(out, "\x1b[2A") {
		t.Error("second render should move the cursor up 2 lines")
	}
	if !strings.Contains(out, "line-c") || !strings.Contains(out, "line-d") {
		t.Error("repaint should contain the new frame")
	}
}

// vScreen replays a live block's ANSI (cursor-up, clear-line, newlines, text)
// onto a virtual screen: row -> current line content. It models exactly what
// Render emits, so the final map is what the terminal would show.
func vScreen(out string) map[int]string {
	screen := map[int]string{}
	row := 0
	for i := 0; i < len(out); {
		switch {
		case strings.HasPrefix(out[i:], "\x1b["):
			j := i + 2
			for j < len(out) && !((out[j] >= 'A' && out[j] <= 'Z') || (out[j] >= 'a' && out[j] <= 'z')) {
				j++
			}
			num := 0
			for k := i + 2; k < j && out[k] >= '0' && out[k] <= '9'; k++ {
				num = num*10 + int(out[k]-'0')
			}
			switch out[j] {
			case 'A':
				row -= num
			case 'K':
				screen[row] = "" // clear line
			}
			i = j + 1
		case out[i] == '\n':
			row++
			i++
		case out[i] == '\r':
			i++
		default:
			j := i
			for j < len(out) && out[j] != '\x1b' && out[j] != '\n' && out[j] != '\r' {
				j++
			}
			screen[row] = out[i:j]
			i = j
		}
	}
	return screen
}

// TestLiveNoDriftOnHeightChange reproduces the -j bug: the block height
// oscillates as the working line comes and goes, and the block must repaint in
// place, never leaving stale copies behind.
func TestLiveNoDriftOnHeightChange(t *testing.T) {
	var buf bytes.Buffer
	l := NewLive(&buf)
	frames := [][]string{
		{"HEADER", "a", "b", "c"},            // 4
		{"HEADER", "a", "b", "c", "working"}, // 5: working appears
		{"HEADER", "a", "b", "c"},            // 4: working gone
		{"HEADER", "a", "b", "c", "working"}, // 5
		{"HEADER", "a", "b", "c"},            // 4
		{"HEADER", "a", "b", "c", "working"}, // 5
	}
	for _, f := range frames {
		l.Render(f)
	}
	screen := vScreen(buf.String())
	headers := 0
	for _, line := range screen {
		if line == "HEADER" {
			headers++
		}
	}
	if headers != 1 {
		t.Errorf("block drifted: %d HEADER lines on screen, want 1 (stale copies stacked)", headers)
	}
}

func TestLiveClose(t *testing.T) {
	var buf bytes.Buffer
	l := NewLive(&buf)
	l.Render([]string{"x"})
	l.Close()
	if !strings.Contains(buf.String(), ansiShowCursor) {
		t.Error("Close should restore the cursor")
	}
}
