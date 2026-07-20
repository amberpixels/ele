package preflight

import (
	"context"
	"os"
	"path/filepath"
	"testing"
)

// TestRunParsesListing: a saved `pg_restore -l` listing is parsed directly, with
// no dump file, no database, and no pg_restore invocation.
func TestRunParsesListing(t *testing.T) {
	res, err := Run(context.Background(), filepath.Join("..", "aggregator", "testdata", "sample.toc"))
	if err != nil {
		t.Fatalf("Run on listing: %v", err)
	}
	pre, data, post, _ := res.Plan.PhaseCounts()
	if pre != 15 || data != 7 || post != 11 {
		t.Errorf("phase counts = %d/%d/%d, want 15/7/11", pre, data, post)
	}
	if res.ByteSized {
		t.Error("a listing carries no byte sizes")
	}
}

// TestLoadListingRejectsBinary: a file with a NUL byte (a real archive) is not
// treated as a listing, so Run falls through to pg_restore.
func TestLoadListingRejectsBinary(t *testing.T) {
	p := filepath.Join(t.TempDir(), "fake.dump")
	if err := os.WriteFile(p, append([]byte("PGDMP"), 0, 1, 2, 3), 0o644); err != nil {
		t.Fatal(err)
	}
	if _, ok := loadListing(p); ok {
		t.Error("binary archive must not be accepted as a listing")
	}
}
