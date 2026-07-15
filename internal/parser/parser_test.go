package parser

import (
	"bufio"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"testing"
)

// replay feeds a captured stderr fixture through the parser and returns every
// event, exactly as the runner will drive it line by line.
func replay(t *testing.T, name string) []Event {
	t.Helper()
	f, err := os.Open(filepath.Join("testdata", name))
	if err != nil {
		t.Fatalf("open fixture: %v", err)
	}
	defer f.Close()

	sc := bufio.NewScanner(f)
	sc.Buffer(make([]byte, 0, 64*1024), 4*1024*1024)

	p := New()
	var evs []Event
	for sc.Scan() {
		evs = append(evs, p.Feed(sc.Text())...)
	}
	return append(evs, p.Flush()...)
}

func counts(evs []Event) map[Kind]int {
	m := map[Kind]int{}
	for _, e := range evs {
		m[e.Kind]++
	}
	return m
}

// reportedErrors returns N from a "warning: errors ignored on restore: N" line,
// pg_restore's own error tally, or -1 if absent.
func reportedErrors(evs []Event) int {
	const marker = "errors ignored on restore: "
	for _, e := range evs {
		if e.Kind == KindWarning && strings.HasPrefix(e.Message, marker) {
			n, _ := strconv.Atoi(strings.TrimSpace(strings.TrimPrefix(e.Message, marker)))
			return n
		}
	}
	return -1
}

func find(evs []Event, pred func(Event) bool) (Event, bool) {
	for _, e := range evs {
		if pred(e) {
			return e, true
		}
	}
	return Event{}, false
}

func TestHappyPath(t *testing.T) {
	evs := replay(t, "serial-happy.stderr")
	c := counts(evs)
	t.Logf("kinds: %v", c)

	if c[KindError] != 0 || c[KindWarning] != 0 || c[KindUnknown] != 0 {
		t.Errorf("happy path should be clean: errors=%d warnings=%d unknown=%d",
			c[KindError], c[KindWarning], c[KindUnknown])
	}
	if c[KindCreating] == 0 {
		t.Error("expected creating events")
	}

	// First object created is the function, with its schema-qualified name.
	if e, ok := find(evs, func(e Event) bool { return e.Kind == KindCreating }); !ok ||
		e.Desc != "FUNCTION" || e.Name != "public.title_len(text)" {
		t.Errorf("first creating = %+v", e)
	}
	// Multi-word desc + composite name (table + constraint) survives.
	if _, ok := find(evs, func(e Event) bool {
		return e.Kind == KindCreating && e.Desc == "CONSTRAINT" && e.Name == "public.authors authors_pkey"
	}); !ok {
		t.Error("missing CONSTRAINT creating event with composite name")
	}
	if _, ok := find(evs, func(e Event) bool {
		return e.Kind == KindProcessingData && e.Name == "public.authors"
	}); !ok {
		t.Error("missing processing-data event for public.authors")
	}
}

func TestCleanSerialErrors(t *testing.T) {
	evs := replay(t, "clean-serial.stderr")
	c := counts(evs)
	t.Logf("kinds: %v", c)

	if c[KindUnknown] != 0 {
		t.Errorf("unexpected unknown lines: %d", c[KindUnknown])
	}
	// Our error count must match pg_restore's own tally.
	if got, want := c[KindError], reportedErrors(evs); want >= 0 && got != want {
		t.Errorf("parsed %d errors, pg_restore reported %d", got, want)
	}

	// Every error must be stitched: dump id from context + failing SQL.
	for _, e := range evs {
		if e.Kind != KindError {
			continue
		}
		if e.DumpID == 0 {
			t.Errorf("error without dump id: %q", e.Message)
		}
		if e.Command == "" {
			t.Errorf("error without Command: %q", e.Message)
		}
	}

	// The first failure, checked end to end.
	e, ok := find(evs, func(e Event) bool { return e.Kind == KindError })
	if !ok {
		t.Fatal("no error events")
	}
	if e.DumpID != 3716 {
		t.Errorf("first error dump id = %d, want 3716", e.DumpID)
	}
	if !strings.Contains(e.Message, `relation "public.book_tags" does not exist`) {
		t.Errorf("first error message = %q", e.Message)
	}
	if e.Command != "ALTER TABLE ONLY public.book_tags DROP CONSTRAINT bt_tag_fk;" {
		t.Errorf("first error command = %q", e.Command)
	}
}

// TestParallelInterleaving is the core robustness test: -j workers double, drop,
// and split the "pg_restore:" prefix mid-line, yet no line may be lost.
func TestParallelInterleaving(t *testing.T) {
	evs := replay(t, "clean-j4.stderr")
	c := counts(evs)
	t.Logf("kinds: %v", c)

	// Nothing falls through despite the torn prefixes.
	if c[KindUnknown] != 0 {
		var samples []string
		for _, e := range evs {
			if e.Kind == KindUnknown {
				samples = append(samples, e.Raw)
			}
		}
		t.Errorf("unknown lines from interleaving (%d): %q", c[KindUnknown], samples)
	}

	// A launch line that arrived with a DOUBLED prefix still parsed (line 138).
	if _, ok := find(evs, func(e Event) bool {
		return e.Kind == KindLaunchItem && e.DumpID == 3703
	}); !ok {
		t.Error("doubled-prefix launch item 3703 not parsed")
	}
	// A creating line that LOST its prefix to interleaving still parsed (line 173).
	if _, ok := find(evs, func(e Event) bool {
		return e.Kind == KindCreating && e.Desc == "RULE" && e.Name == "public.author_book_counts _RETURN"
	}); !ok {
		t.Error("prefix-less RULE creating line not parsed")
	}
	// An executing line that lost its prefix still parsed (line 139).
	if _, ok := find(evs, func(e Event) bool {
		return e.Kind == KindExecuting && e.Name == "authors_id_seq"
	}); !ok {
		t.Error("prefix-less executing line not parsed")
	}

	// Every launched item is later reported finished (keyed by dump id, never
	// by line adjacency).
	launched, finished := map[int]bool{}, map[int]bool{}
	for _, e := range evs {
		switch e.Kind {
		case KindLaunchItem:
			launched[e.DumpID] = true
		case KindFinishItem:
			finished[e.DumpID] = true
		}
	}
	if len(launched) == 0 {
		t.Fatal("no launched items")
	}
	for id := range launched {
		if !finished[id] {
			t.Errorf("item %d launched but never finished", id)
		}
	}

	if got, want := c[KindError], reportedErrors(evs); want >= 0 && got != want {
		t.Errorf("parsed %d errors, pg_restore reported %d", got, want)
	}
}
