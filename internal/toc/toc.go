// Package toc parses `pg_restore -l` output into a RestorePlan: the per-phase
// denominators and object index ele draws progress against. It does no I/O
// beyond reading the listing.
package toc

import (
	"bufio"
	"fmt"
	"io"
	"strconv"
	"strings"
)

// Entry is one object in the dump's table of contents. A listing line:
//
//	4012; 0 16385 TABLE DATA public activity_logs app_owner
//
// is dumpID; tableOID objectOID <desc> <schema> <tag> <owner>. desc and tag
// may contain spaces; the section is not printed - see parseLine.
type Entry struct {
	DumpID   int     // dump id; matches the ids pg_restore prints under -j
	TableOID uint32  // catalog table oid
	OID      uint32  // object oid
	Desc     string  // object type, e.g. "TABLE", "TABLE DATA", "FK CONSTRAINT"
	Section  Section // derived from Desc; not present in the listing
	Schema   string  // namespace; "-" in the listing becomes ""
	Tag      string  // object name / identifier (may contain spaces)
	Owner    string  // owning role; "" for pseudo-entries
	Bytes    int64   // on-disk data size, if known (dir format); else 0
	HasBytes bool    // whether Bytes was populated from a stat
}

// RestorePlan is the parsed, indexed table of contents, built once by Parse.
type RestorePlan struct {
	Entries []Entry

	byID map[int]int // DumpID -> index into Entries
}

// Parse reads a `pg_restore -l` listing into a RestorePlan. Comment (';') and
// blank lines are ignored; a malformed entry line is skipped rather than
// failing the parse, so one odd line never aborts preflight.
func Parse(r io.Reader) (*RestorePlan, error) {
	plan := &RestorePlan{byID: map[int]int{}}

	sc := bufio.NewScanner(r)
	// TOC tags can be long (function signatures); raise the 64 KiB line cap.
	sc.Buffer(make([]byte, 0, 64*1024), 4*1024*1024)

	for sc.Scan() {
		line := sc.Text()
		trimmed := strings.TrimSpace(line)
		if trimmed == "" || strings.HasPrefix(trimmed, ";") {
			continue
		}
		e, ok := parseLine(trimmed)
		if !ok {
			continue
		}
		plan.byID[e.DumpID] = len(plan.Entries)
		plan.Entries = append(plan.Entries, e)
	}
	if err := sc.Err(); err != nil {
		return nil, fmt.Errorf("reading toc listing: %w", err)
	}
	return plan, nil
}

// parseLine parses one listing line into an Entry, returning ok=false for
// non-entry lines. Two fields are variable-width: desc can be up to three
// words ("TEXT SEARCH CONFIGURATION") and tag can contain spaces
// ("fn(integer, text)"). We anchor on both ends: desc is a known set, so take
// the longest matching prefix; owner is the final token; the tag is whatever
// sits between schema and owner.
func parseLine(line string) (Entry, bool) {
	semi := strings.IndexByte(line, ';')
	if semi <= 0 {
		return Entry{}, false
	}
	dumpID, err := strconv.Atoi(strings.TrimSpace(line[:semi]))
	if err != nil {
		return Entry{}, false
	}

	fields := strings.Fields(line[semi+1:])
	if len(fields) < 3 { // need at least: tableOID objectOID desc
		return Entry{}, false
	}

	tableOID, err1 := strconv.ParseUint(fields[0], 10, 32)
	objOID, err2 := strconv.ParseUint(fields[1], 10, 32)
	if err1 != nil || err2 != nil {
		return Entry{}, false
	}

	rest := fields[2:] // <desc...> <schema> <tag...> <owner>

	desc, section, n := matchDesc(rest)
	rest = rest[n:]
	if len(rest) == 0 {
		// desc with no schema/tag; keep it minimally.
		return Entry{
			DumpID: dumpID, TableOID: uint32(tableOID), OID: uint32(objOID),
			Desc: desc, Section: section,
		}, true
	}

	schema := rest[0]
	if schema == "-" {
		schema = ""
	}
	rest = rest[1:]

	var tag, owner string
	switch len(rest) {
	case 0:
		// schema only; leave tag/owner blank.
	case 1:
		// One token: it's the tag, owner blank (ENCODING/STDSTRINGS pseudo-entries).
		tag = rest[0]
	default:
		owner = rest[len(rest)-1]
		tag = strings.Join(rest[:len(rest)-1], " ")
	}

	return Entry{
		DumpID:   dumpID,
		TableOID: uint32(tableOID),
		OID:      uint32(objOID),
		Desc:     desc,
		Section:  section,
		Schema:   schema,
		Tag:      tag,
		Owner:    owner,
	}, true
}

// matchDesc consumes the longest known description from the front of toks,
// returning it, its section, and the token count. An unknown leading token
// yields a one-word SectionUnknown desc so the entry still survives.
func matchDesc(toks []string) (desc string, section Section, n int) {
	limit := min(maxDescWords, len(toks))
	for k := limit; k >= 1; k-- {
		candidate := strings.Join(toks[:k], " ")
		if s, ok := descSection[candidate]; ok {
			return candidate, s, k
		}
	}
	return toks[0], SectionUnknown, 1
}

// ParseListingLine parses a single `pg_restore -l` entry line - also the tail
// of pg_restore's "from TOC entry ..." error-context line, which has the same
// shape. ok is false for anything that isn't a TOC entry.
func ParseListingLine(line string) (Entry, bool) { return parseLine(line) }

// MatchDesc consumes the longest known object description from the front of
// toks, returning it and how many tokens it spanned. It returns ("", 0) for an
// empty input and a one-token desc when nothing matches.
func MatchDesc(toks []string) (desc string, n int) {
	if len(toks) == 0 {
		return "", 0
	}
	d, _, spanned := matchDesc(toks)
	return d, spanned
}

// SectionOf returns the restore section for an object description, or
// SectionUnknown if the description isn't recognised.
func SectionOf(desc string) Section {
	if s, ok := descSection[desc]; ok {
		return s
	}
	return SectionUnknown
}

// Get returns the entry with the given dump id and whether it was found.
func (p *RestorePlan) Get(dumpID int) (Entry, bool) {
	i, ok := p.byID[dumpID]
	if !ok {
		return Entry{}, false
	}
	return p.Entries[i], true
}

// SetBytes records an on-disk data size for a dump id (directory-format dumps).
// No-op for unknown ids.
func (p *RestorePlan) SetBytes(dumpID int, bytes int64) {
	if i, ok := p.byID[dumpID]; ok {
		p.Entries[i].Bytes = bytes
		p.Entries[i].HasBytes = true
	}
}

// PhaseCounts returns per-section entry counts - the exact progress denominators.
func (p *RestorePlan) PhaseCounts() (pre, data, post, unknown int) {
	for i := range p.Entries {
		switch p.Entries[i].Section {
		case PreData:
			pre++
		case Data:
			data++
		case PostData:
			post++
		default:
			unknown++
		}
	}
	return pre, data, post, unknown
}

// DataBytes sums the known on-disk size of data-section entries. Only sized
// entries contribute; file-less ones (SEQUENCE SET) add nothing. Whether to
// show bytes at all is preflight's call (ByteSized).
func (p *RestorePlan) DataBytes() int64 {
	var total int64
	for i := range p.Entries {
		if p.Entries[i].Section == Data && p.Entries[i].HasBytes {
			total += p.Entries[i].Bytes
		}
	}
	return total
}
