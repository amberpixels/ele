package aggregator

import (
	"bufio"
	"os"
	"path/filepath"
	"reflect"
	"testing"
	"time"

	"github.com/amberpixels/ele/internal/parser"
	"github.com/amberpixels/ele/internal/toc"
)

func loadPlan(t *testing.T) *toc.RestorePlan {
	t.Helper()
	f, err := os.Open(filepath.Join("testdata", "sample.toc"))
	if err != nil {
		t.Fatalf("open plan: %v", err)
	}
	defer f.Close()
	plan, err := toc.Parse(f)
	if err != nil {
		t.Fatalf("parse plan: %v", err)
	}
	return plan
}

// replay drives a captured stderr fixture (from the parser package's testdata)
// through parser then aggregator, exactly as the runner will wire them.
func replay(t *testing.T, fixture string, cfg Config) *Aggregator {
	t.Helper()
	f, err := os.Open(filepath.Join("..", "parser", "testdata", fixture))
	if err != nil {
		t.Fatalf("open fixture: %v", err)
	}
	defer f.Close()

	agg := New(loadPlan(t), cfg)
	p := parser.New()
	sc := bufio.NewScanner(f)
	sc.Buffer(make([]byte, 0, 64*1024), 4*1024*1024)
	for sc.Scan() {
		for _, ev := range p.Feed(sc.Text()) {
			agg.Feed(ev)
		}
	}
	for _, ev := range p.Flush() {
		agg.Feed(ev)
	}
	return agg
}

// assertProgressSane checks the invariant that must always hold: no phase is
// ever over 100%.
func assertProgressSane(t *testing.T, s Snapshot) {
	t.Helper()
	for _, p := range []PhaseProgress{s.Pre, s.Data, s.Post} {
		if p.Done > p.Total {
			t.Errorf("%s over 100%%: %d/%d", p.Section, p.Done, p.Total)
		}
	}
}

func TestAggregateParallel(t *testing.T) {
	s := replay(t, "clean-j4.stderr", Config{Clean: true, NoOwner: true}).Snapshot()
	t.Logf("pre %d/%d  data %d/%d  post %d/%d  errs %d (real %d, benign %d)  unknown %d",
		s.Pre.Done, s.Pre.Total, s.Data.Done, s.Data.Total, s.Post.Done, s.Post.Total,
		s.ErrTotal, s.ErrReal, s.ErrBenign, s.Unknown)

	assertProgressSane(t, s)

	// Every data and post-data item finishes under -j, so both phases complete.
	if s.Data.Done != s.Data.Total {
		t.Errorf("data = %d/%d, want complete", s.Data.Done, s.Data.Total)
	}
	if s.Post.Done != s.Post.Total {
		t.Errorf("post = %d/%d, want complete", s.Post.Done, s.Post.Total)
	}
	if s.Pre.Done == 0 {
		t.Error("pre-data made no progress")
	}

	// All launched items were reconciled as finished.
	if len(s.InFlight) != 0 {
		t.Errorf("in-flight not drained: %+v", s.InFlight)
	}

	// The 21 does-not-exist errors are all benign under --clean, in 2 templates
	// (relation / index), so the exit code would normalize to success.
	if s.ErrTotal != 21 || s.ErrReal != 0 || s.ErrBenign != 21 {
		t.Errorf("errors = total %d real %d benign %d, want 21/0/21", s.ErrTotal, s.ErrReal, s.ErrBenign)
	}
	// 5 templates: relation/index/sequence/table (quoted) + function (unquoted
	// signature), each a distinct fingerprint, all benign, summing to 21.
	if len(s.Errors) != 5 {
		t.Errorf("error groups = %d, want 5", len(s.Errors))
	}
	sum := 0
	for _, g := range s.Errors {
		sum += g.Count
		if !g.Benign {
			t.Errorf("group not benign: %q", g.Sample)
		}
	}
	if sum != 21 {
		t.Errorf("group counts sum to %d, want 21", sum)
	}
	if s.Unknown != 0 {
		t.Errorf("unknown lines = %d", s.Unknown)
	}
}

func TestAggregateSerial(t *testing.T) {
	s := replay(t, "clean-serial.stderr", Config{Clean: true, NoOwner: true}).Snapshot()
	t.Logf("pre %d/%d  data %d/%d  post %d/%d", s.Pre.Done, s.Pre.Total, s.Data.Done, s.Data.Total, s.Post.Done, s.Post.Total)

	assertProgressSane(t, s)

	// Serial mode counts creating / processing-data / executing lines; the data
	// phase (4 tables + 3 sequence sets) completes from those.
	if s.Data.Done != s.Data.Total {
		t.Errorf("data = %d/%d, want complete", s.Data.Done, s.Data.Total)
	}
	if s.Pre.Done == 0 || s.Post.Done == 0 {
		t.Errorf("phases stalled: pre %d post %d", s.Pre.Done, s.Post.Done)
	}
	if s.ErrReal != 0 || s.ErrBenign != 21 {
		t.Errorf("errors real %d benign %d, want 0/21", s.ErrReal, s.ErrBenign)
	}
	if s.Unknown != 0 {
		t.Errorf("unknown lines = %d", s.Unknown)
	}
}

func TestHappyPathNoErrors(t *testing.T) {
	s := replay(t, "serial-happy.stderr", Config{}).Snapshot()
	assertProgressSane(t, s)
	if s.ErrTotal != 0 {
		t.Errorf("happy path has errors: %d", s.ErrTotal)
	}
	if s.Data.Done != s.Data.Total {
		t.Errorf("data = %d/%d, want complete", s.Data.Done, s.Data.Total)
	}
}

func TestTimingAndInFlight(t *testing.T) {
	base := time.Unix(1_000_000, 0)
	clock := base
	a := New(loadPlan(t), Config{})
	a.now = func() time.Time { return clock }

	// One item finishes after 2s; another is still in flight at +5s.
	a.Feed(parser.Event{Kind: parser.KindLaunchItem, DumpID: 3864, Desc: "TABLE DATA", Tag: "authors"})
	a.Feed(parser.Event{Kind: parser.KindLaunchItem, DumpID: 3866, Desc: "TABLE DATA", Tag: "books"})
	clock = base.Add(2 * time.Second)
	a.Feed(parser.Event{Kind: parser.KindFinishItem, DumpID: 3864, Desc: "TABLE DATA", Tag: "authors"})
	clock = base.Add(5 * time.Second)

	s := a.Snapshot()
	if len(s.Slowest) != 1 || s.Slowest[0].Dur != 2*time.Second || s.Slowest[0].Tag != "authors" {
		t.Errorf("slowest = %+v, want one 2s authors", s.Slowest)
	}
	if len(s.InFlight) != 1 || s.InFlight[0].DumpID != 3866 || s.InFlight[0].Elapsed != 5*time.Second {
		t.Errorf("in-flight = %+v, want books at 5s", s.InFlight)
	}
	if s.Data.Done != 1 {
		t.Errorf("data done = %d, want 1 (only authors finished)", s.Data.Done)
	}
}

func TestFingerprint(t *testing.T) {
	tmpl, params := fingerprint(`could not execute query: ERROR:  relation "public.books" does not exist`)
	if want := `could not execute query: ERROR:  relation "…" does not exist`; tmpl != want {
		t.Errorf("template = %q, want %q", tmpl, want)
	}
	if !reflect.DeepEqual(params, []string{"public.books"}) {
		t.Errorf("params = %v", params)
	}
}

func TestClassifyBenign(t *testing.T) {
	dropNoise := `could not execute query: ERROR:  relation "public.books" does not exist`
	dropCmd := "ALTER TABLE ONLY public.books DROP CONSTRAINT books_pkey;"
	if !classifyBenign(dropNoise, dropCmd, true, false) {
		t.Error("clean DROP does-not-exist should be benign")
	}
	if classifyBenign(dropNoise, dropCmd, false, false) {
		t.Error("without --clean the same error is real")
	}
	// A real failure (unique violation) is never benign.
	if classifyBenign(`duplicate key value violates unique constraint "x"`, "INSERT ...", true, true) {
		t.Error("unique violation must be real")
	}
}
