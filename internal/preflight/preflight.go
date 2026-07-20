// Package preflight builds a toc.RestorePlan before the restore runs. Given a
// real archive it runs `pg_restore -l` (and, for directory-format dumps, sizes
// each table's data file); given a saved `pg_restore -l` listing it parses that
// directly, so callers can work offline without the dump on hand.
package preflight

import (
	"bytes"
	"context"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"

	"github.com/amberpixels/ele/internal/toc"
)

// Result is the parsed plan plus whether byte sizes were attached (directory
// dumps only).
type Result struct {
	Plan      *toc.RestorePlan
	Format    Format
	ByteSized bool // true when data-phase progress can be shown in bytes
}

// Format is the dump's archive format, as far as preflight can tell.
type Format uint8

const (
	FormatUnknown   Format = iota
	FormatCustom           // -Fc: single file
	FormatTar              // -Ft: single .tar file
	FormatDirectory        // -Fd: a directory with toc.dat + per-entry data files
)

func (f Format) String() string {
	switch f {
	case FormatCustom:
		return "custom"
	case FormatTar:
		return "tar"
	case FormatDirectory:
		return "directory"
	default:
		return "unknown"
	}
}

// Run executes `pg_restore -l dumpPath` (LC_ALL=C for a stable listing) and
// parses it, sizing directory-format data files for byte-based progress.
// dumpPath is the archive being restored; empty means stdin, where preflight
// is impossible and Run returns an error for the caller to degrade.
func Run(ctx context.Context, dumpPath string) (*Result, error) {
	if dumpPath == "" {
		return nil, fmt.Errorf("preflight: no dump path (stdin restore); caller must degrade")
	}

	// A saved `pg_restore -l` listing is plain text we can parse without a dump
	// file or database. A real (binary) archive isn't a valid listing and falls
	// through to running pg_restore -l on it.
	if res, ok := loadListing(dumpPath); ok {
		return res, nil
	}

	format := detectFormat(dumpPath)

	cmd := exec.CommandContext(ctx, "pg_restore", "-l", dumpPath)
	// LC_ALL=C keeps the header wording deterministic (the -l body is
	// identifiers only).
	cmd.Env = append(os.Environ(), "LC_ALL=C")

	var stdout, stderr bytes.Buffer
	cmd.Stdout = &stdout
	cmd.Stderr = &stderr
	if err := cmd.Run(); err != nil {
		return nil, fmt.Errorf("pg_restore -l %s: %w: %s", dumpPath, err, stderr.String())
	}

	plan, err := toc.Parse(&stdout)
	if err != nil {
		return nil, fmt.Errorf("preflight: %w", err)
	}

	res := &Result{Plan: plan, Format: format}
	if format == FormatDirectory {
		res.ByteSized = sizeDirectoryData(dumpPath, plan)
	}
	return res, nil
}

// loadListing parses source as a saved `pg_restore -l` listing, returning a
// Result (a listing carries no format or byte sizes) and true when it holds at
// least one TOC entry. Binary archives are rejected cheaply by a leading-NUL
// check, so a multi-GB dump isn't scanned line by line only to find nothing.
func loadListing(source string) (*Result, bool) {
	f, err := os.Open(source)
	if err != nil {
		return nil, false
	}
	defer f.Close()

	head := make([]byte, 512)
	n, _ := f.Read(head)
	if bytes.IndexByte(head[:n], 0) >= 0 { // NUL byte => binary archive, not a listing
		return nil, false
	}
	if _, err := f.Seek(0, io.SeekStart); err != nil {
		return nil, false
	}

	plan, err := toc.Parse(f)
	if err != nil || len(plan.Entries) == 0 {
		return nil, false
	}
	return &Result{Plan: plan, Format: FormatUnknown}, true
}

// detectFormat guesses from the path: directory -Fd, .tar -Ft, else custom.
// This only steers byte sizing; a wrong guess degrades to counts, never fails.
func detectFormat(dumpPath string) Format {
	info, err := os.Stat(dumpPath)
	if err != nil {
		return FormatUnknown
	}
	if info.IsDir() {
		return FormatDirectory
	}
	if filepath.Ext(dumpPath) == ".tar" {
		return FormatTar
	}
	return FormatCustom
}

// sizeDirectoryData stats each TABLE DATA entry's file (named "<dumpID>.dat"
// with an optional compression suffix). Returns true if every one was sized.
func sizeDirectoryData(dir string, plan *toc.RestorePlan) bool {
	allSized := true
	for _, e := range plan.Entries {
		if e.Section != toc.Data || e.Desc != "TABLE DATA" {
			continue
		}
		size, ok := statDataFile(dir, e.DumpID)
		if !ok {
			allSized = false
			continue
		}
		plan.SetBytes(e.DumpID, size)
	}
	return allSized
}

// statDataFile stats an entry's data file, trying each compression suffix.
func statDataFile(dir string, dumpID int) (int64, bool) {
	base := filepath.Join(dir, strconv.Itoa(dumpID)+".dat")
	for _, suffix := range []string{"", ".gz", ".lz4", ".zst"} {
		if info, err := os.Stat(base + suffix); err == nil {
			return info.Size(), true
		}
	}
	return 0, false
}
