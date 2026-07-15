package render

import (
	"bytes"
	"strings"
	"testing"

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

func TestFrameInFlight(t *testing.T) {
	s := sample()
	s.InFlight = []aggregator.InFlightItem{
		{Tag: "big_table"}, {Tag: "other"}, {Tag: "third"}, {Tag: "fourth"},
	}
	out := joined(Frame(s, Opts{Spinner: '⣾'}))
	if !strings.Contains(out, "big_table") || !strings.Contains(out, "+1 in flight") {
		t.Errorf("in-flight line wrong:\n%s", out)
	}
}

func TestFrameGroupCap(t *testing.T) {
	s := sample()
	// Seven groups, but only maxGroups (5) are listed.
	s.Errors = nil
	for range 7 {
		s.Errors = append(s.Errors, aggregator.ErrorGroup{Template: "e", Count: 1, Benign: true})
	}
	shown := 0
	for _, l := range Frame(s, Opts{}) {
		if strings.Contains(l, "benign  e  ×1") {
			shown++
		}
	}
	if shown != maxGroups {
		t.Errorf("listed %d groups, want %d", shown, maxGroups)
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
	Summary(&buf, sample(), "run.log", 0)
	out := buf.String()
	if !strings.Contains(out, "success - 1909 benign error(s) normalized to exit 0") {
		t.Errorf("outcome wrong:\n%s", out)
	}

	buf.Reset()
	s := sample()
	s.ErrReal = 2
	Summary(&buf, s, "", 0)
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

	pp.Update(snap(0, 0, 0))    // first tick -> emits
	pp.Update(snap(50, 0, 0))   // no milestone (pre incomplete, deciles unchanged)
	pp.Update(snap(100, 0, 0))  // pre completes -> emits
	pp.Update(snap(100, 10, 0)) // data crosses 10% -> emits
	pp.Update(snap(100, 15, 0)) // same decile -> silent
	pp.Update(snap(100, 20, 0)) // data crosses 20% -> emits

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

	l.Close()
	if !strings.Contains(buf.String(), ansiShowCursor) {
		t.Error("Close should restore the cursor")
	}
}
