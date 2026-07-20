package engine

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

const (
	tocFixture = "../aggregator/testdata/sample.toc"
	logFixture = "../parser/testdata/clean-serial.stderr"
)

// runReplay drives Replay with a temp file standing in for the terminal (not a
// TTY, so it renders the deterministic plain summary) and returns the captured
// output and exit code.
func runReplay(t *testing.T, planSource, logPath string) (string, int) {
	t.Helper()
	out, err := os.CreateTemp(t.TempDir(), "replay-*.out")
	if err != nil {
		t.Fatal(err)
	}
	defer out.Close()

	code, rerr := Replay(context.Background(), planSource, logPath, os.Stdout, out)
	if rerr != nil {
		t.Fatalf("Replay error: %v", rerr)
	}
	b, err := os.ReadFile(out.Name())
	if err != nil {
		t.Fatal(err)
	}
	return string(b), code
}

// TestReplayFromListing is the offline dry-run: a saved `pg_restore -l` listing
// plus a captured stderr log, no database and no dump. It must reproduce the
// summary and normalize the all-benign exit to 0.
func TestReplayFromListing(t *testing.T) {
	out, code := runReplay(t, tocFixture, logFixture)
	if code != 0 {
		t.Errorf("exit = %d, want 0 (all errors benign)", code)
	}
	for _, want := range []string{
		"success", "21 benign", // classified error verdict
		"15/15", "7/7", "11/11", // phases complete against the matched plan
	} {
		if !strings.Contains(out, want) {
			t.Errorf("summary missing %q:\n%s", want, out)
		}
	}
}

// TestReplayMissingLog surfaces a missing log as an error and a failing code,
// never a panic.
func TestReplayMissingLog(t *testing.T) {
	code, err := Replay(context.Background(), tocFixture, filepath.Join(t.TempDir(), "nope.log"), os.Stdout, os.Stderr)
	if err == nil || code == 0 {
		t.Errorf("missing log: code=%d err=%v, want nonzero code and an error", code, err)
	}
}

// TestLoadPlanListing confirms a text listing is parsed directly (no dump, no
// pg_restore) and yields the expected denominators.
func TestLoadPlanListing(t *testing.T) {
	plan, err := loadPlan(context.Background(), tocFixture)
	if err != nil {
		t.Fatal(err)
	}
	pre, data, post, _ := plan.PhaseCounts()
	if pre != 15 || data != 7 || post != 11 {
		t.Errorf("phase counts = %d/%d/%d, want 15/7/11", pre, data, post)
	}
}
