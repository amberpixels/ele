// Command pgmsg regenerates the parser's message table from PostgreSQL's own
// message catalogs, so a new major release is a data change rather than a
// guessing game about which wording moved.
//
// It pulls the msgid set of src/bin/pg_dump/po/<lang>.po at each supported
// release tag from the GitHub mirror, snapshots it under testdata/, checks the
// hand-maintained bindings (bindings.go) still exist upstream, and writes
// internal/parser/messages_gen.go.
//
//	go run ./tools/pgmsg              # fetch, snapshot, generate
//	go run ./tools/pgmsg -offline     # generate from the committed snapshots
//
// The .po files are the source rather than pg_dump.pot: the .pot template is a
// build artifact and is not committed upstream (404 at every release tag),
// while the .po translations carry the identical msgid set. Two languages are
// unioned per tag, since one translation can lag upstream but two are unlikely
// to lag the same message.
package main

import (
	"flag"
	"fmt"
	"go/format"
	"io"
	"log"
	"net/http"
	"os"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
	"time"
)

const poURL = "https://raw.githubusercontent.com/postgres/postgres/%s/src/bin/pg_dump/po/%s.po"

func main() {
	log.SetFlags(0)
	log.SetPrefix("pgmsg: ")

	tags := flag.String("tags", "REL_13_STABLE,REL_14_STABLE,REL_15_STABLE,REL_16_STABLE,REL_17_STABLE,REL_18_STABLE",
		"release tags to read, oldest first")
	langs := flag.String("langs", "de,fr", "translations to union per tag")
	snapshots := flag.String("snapshots", filepath.Join("tools", "pgmsg", "testdata"), "where msgid snapshots live")
	out := flag.String("out", filepath.Join("internal", "parser", "messages_gen.go"), "generated file")
	offline := flag.Bool("offline", false, "read the committed snapshots instead of fetching")
	flag.Parse()

	tagList := split(*tags)
	langList := split(*langs)
	if len(tagList) == 0 || len(langList) == 0 {
		log.Fatal("need at least one tag and one language")
	}

	catalogs, err := load(tagList, langList, *snapshots, *offline)
	if err != nil {
		log.Fatal(err)
	}
	reportDrift(tagList, catalogs)

	entries, err := resolve(tagList, catalogs)
	if err != nil {
		log.Fatal(err)
	}
	if err := write(*out, entries, tagList, langList); err != nil {
		log.Fatal(err)
	}
	log.Printf("wrote %s: %d message(s) from %d binding(s)", *out, len(entries), len(bindings))
}

// --- catalogs -------------------------------------------------------------

// load returns the msgid set per tag, fetching and snapshotting it unless the
// committed snapshots were asked for.
func load(tags, langs []string, dir string, offline bool) (map[string]map[string]bool, error) {
	catalogs := map[string]map[string]bool{}
	for _, tag := range tags {
		if offline {
			set, err := readSnapshot(filepath.Join(dir, tag+".txt"))
			if err != nil {
				return nil, err
			}
			catalogs[tag] = set
			continue
		}

		set := map[string]bool{}
		for _, lang := range langs {
			body, err := fetch(fmt.Sprintf(poURL, tag, lang))
			if err != nil {
				return nil, fmt.Errorf("%s/%s.po: %w", tag, lang, err)
			}
			for _, id := range msgids(body) {
				set[id] = true
			}
		}
		if len(set) == 0 {
			return nil, fmt.Errorf("%s: no msgids found", tag)
		}
		if err := writeSnapshot(filepath.Join(dir, tag+".txt"), tag, langs, set); err != nil {
			return nil, err
		}
		catalogs[tag] = set
		log.Printf("%s: %d msgids", tag, len(set))
	}
	return catalogs, nil
}

func fetch(url string) (string, error) {
	client := &http.Client{Timeout: 60 * time.Second}
	resp, err := client.Get(url)
	if err != nil {
		return "", err
	}
	defer func() { _ = resp.Body.Close() }()
	if resp.StatusCode != http.StatusOK {
		return "", fmt.Errorf("GET %s: %s", url, resp.Status)
	}
	b, err := io.ReadAll(resp.Body)
	return string(b), err
}

// msgids extracts every msgid from a .po file. gettext wraps long strings over
// several lines (`msgid ""` followed by quoted fragments), so a msgid is read
// until the first line that isn't another fragment.
func msgids(po string) []string {
	var out []string
	lines := strings.Split(po, "\n")
	for i := 0; i < len(lines); i++ {
		line := strings.TrimSpace(lines[i])
		if !strings.HasPrefix(line, "msgid ") {
			continue
		}
		var b strings.Builder
		b.WriteString(unquotePO(strings.TrimPrefix(line, "msgid ")))
		for i+1 < len(lines) {
			next := strings.TrimSpace(lines[i+1])
			if !strings.HasPrefix(next, `"`) {
				break
			}
			b.WriteString(unquotePO(next))
			i++
		}
		if s := b.String(); s != "" {
			out = append(out, s)
		}
	}
	return out
}

// unquotePO resolves one quoted .po fragment, including the C escapes gettext
// uses. Anything that isn't a quoted fragment yields nothing.
func unquotePO(s string) string {
	s = strings.TrimSpace(s)
	if len(s) < 2 || s[0] != '"' || s[len(s)-1] != '"' {
		return ""
	}
	return strings.NewReplacer(`\"`, `"`, `\\`, `\`, `\n`, "\n", `\t`, "\t").Replace(s[1 : len(s)-1])
}

// --- snapshots ------------------------------------------------------------

// writeSnapshot records a tag's msgid set, one strconv-quoted msgid per line so
// multi-line messages stay on one line and the file diffs cleanly. Sorted, with
// no timestamp, so regenerating an unchanged tag is a no-op.
func writeSnapshot(path, tag string, langs []string, set map[string]bool) error {
	var b strings.Builder
	fmt.Fprintf(&b, "# msgid set of src/bin/pg_dump/po/{%s}.po at %s\n", strings.Join(langs, ","), tag)
	fmt.Fprintf(&b, "# generated by tools/pgmsg; one Go-quoted msgid per line\n")
	for _, id := range sorted(set) {
		b.WriteString(strconv.Quote(id) + "\n")
	}
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return err
	}
	return os.WriteFile(path, []byte(b.String()), 0o644)
}

func readSnapshot(path string) (map[string]bool, error) {
	b, err := os.ReadFile(path)
	if err != nil {
		return nil, fmt.Errorf("reading snapshot (run without -offline to create it): %w", err)
	}
	set := map[string]bool{}
	for _, line := range strings.Split(string(b), "\n") {
		line = strings.TrimSpace(line)
		if line == "" || strings.HasPrefix(line, "#") {
			continue
		}
		id, err := strconv.Unquote(line)
		if err != nil {
			return nil, fmt.Errorf("%s: %w", path, err)
		}
		set[id] = true
	}
	return set, nil
}

// reportDrift prints how the catalog moved between adjacent releases - the
// cheap early warning that a new major reworded something.
func reportDrift(tags []string, catalogs map[string]map[string]bool) {
	for i := 1; i < len(tags); i++ {
		prev, cur := catalogs[tags[i-1]], catalogs[tags[i]]
		added, removed := 0, 0
		for id := range cur {
			if !prev[id] {
				added++
			}
		}
		for id := range prev {
			if !cur[id] {
				removed++
			}
		}
		log.Printf("%s -> %s: +%d -%d msgids", tags[i-1], tags[i], added, removed)
	}
}

// --- resolution -----------------------------------------------------------

// entry is one row of the generated table: the literal text to match, what it
// means, and the msgids (with their release ranges) that produced it.
type entry struct {
	literal string
	exact   bool
	kind    string
	shape   string
	sources []string // "msgid" (range), for the row's comment
}

// resolve checks every binding against the catalogs and folds the bindings that
// share a literal into one row. A binding no release carries is an error: it
// means the parser is matching on wording that no longer exists.
func resolve(tags []string, catalogs map[string]map[string]bool) ([]entry, error) {
	byLiteral := map[string]*entry{}
	var order []string
	var missing []string

	for _, b := range bindings {
		var found []string
		for _, tag := range tags {
			if catalogs[tag][b.msgid] {
				found = append(found, tag)
			}
		}
		if len(found) == 0 {
			missing = append(missing, b.msgid)
			continue
		}

		lit, exact := literal(b.msgid)
		e := byLiteral[lit]
		if e == nil {
			e = &entry{literal: lit, exact: exact, kind: b.kind, shape: b.shape}
			byLiteral[lit] = e
			order = append(order, lit)
		}
		if e.kind != b.kind || e.shape != b.shape {
			return nil, fmt.Errorf("%q: two bindings share a literal but disagree (%s/%s vs %s/%s)",
				lit, e.kind, e.shape, b.kind, b.shape)
		}
		e.sources = append(e.sources, fmt.Sprintf("%s (%s)", strconv.Quote(b.msgid), versions(found)))
	}

	if len(missing) > 0 {
		return nil, fmt.Errorf("binding(s) absent from every release - upstream reworded them:\n  %s",
			strings.Join(missing, "\n  "))
	}

	// Longest literal first: the most specific match wins, whatever order the
	// bindings happen to be written in.
	sort.SliceStable(order, func(i, j int) bool {
		if len(order[i]) != len(order[j]) {
			return len(order[i]) > len(order[j])
		}
		return order[i] < order[j]
	})
	out := make([]entry, 0, len(order))
	for _, lit := range order {
		out = append(out, *byLiteral[lit])
	}
	return out, nil
}

// literal is the text pg_restore prints verbatim: everything before the first
// format verb. A msgid with no verb is matched exactly rather than by prefix.
//
// A quote right before the verb belongs to the argument, not to the literal:
// postgres quotes every identifier, and the parser's unquote is what reads that
// back. Leaving it in the literal would also make the match intolerant of a
// release that stopped quoting there.
func literal(msgid string) (lit string, exact bool) {
	if i := strings.IndexByte(msgid, '%'); i >= 0 {
		return strings.TrimSuffix(msgid[:i], `"`), false
	}
	return msgid, true
}

// versions renders the majors a msgid was found in, compressing runs: "13-18",
// "17-18", "13, 16-18".
func versions(tags []string) string {
	var majors []string
	for _, t := range tags {
		majors = append(majors, strings.TrimSuffix(strings.TrimPrefix(t, "REL_"), "_STABLE"))
	}
	var parts []string
	for i := 0; i < len(majors); {
		j := i
		for j+1 < len(majors) && consecutive(majors[j], majors[j+1]) {
			j++
		}
		if i == j {
			parts = append(parts, majors[i])
		} else {
			parts = append(parts, majors[i]+"-"+majors[j])
		}
		i = j + 1
	}
	return strings.Join(parts, ", ")
}

func consecutive(a, b string) bool {
	x, err1 := strconv.Atoi(a)
	y, err2 := strconv.Atoi(b)
	return err1 == nil && err2 == nil && y == x+1
}

// --- output ---------------------------------------------------------------

func write(path string, entries []entry, tags, langs []string) error {
	var b strings.Builder
	b.WriteString("// Code generated by tools/pgmsg. DO NOT EDIT.\n//\n")
	fmt.Fprintf(&b, "// Source: src/bin/pg_dump/po/{%s}.po at %s .. %s\n",
		strings.Join(langs, ","), tags[0], tags[len(tags)-1])
	b.WriteString("// (github.com/postgres/postgres). Regenerate with `just gen-messages`.\n\n")
	b.WriteString("package parser\n\n")
	b.WriteString("// messages is what pg_restore prints, mapped to what it means. Ordered\n")
	b.WriteString("// longest-literal-first, so the first match is the most specific one.\n")
	b.WriteString("var messages = []message{\n")
	for _, e := range entries {
		fmt.Fprintf(&b, "\t// %s\n", strings.Join(e.sources, ", "))
		fmt.Fprintf(&b, "\t{literal: %s, exact: %t, kind: %s, shape: %s},\n",
			strconv.Quote(e.literal), e.exact, e.kind, e.shape)
	}
	b.WriteString("}\n")

	src, err := format.Source([]byte(b.String()))
	if err != nil {
		return fmt.Errorf("formatting generated source: %w", err)
	}
	return os.WriteFile(path, src, 0o644)
}

// --- helpers --------------------------------------------------------------

func split(s string) []string {
	var out []string
	for _, p := range strings.Split(s, ",") {
		if p = strings.TrimSpace(p); p != "" {
			out = append(out, p)
		}
	}
	return out
}

func sorted(set map[string]bool) []string {
	out := make([]string, 0, len(set))
	for k := range set {
		out = append(out, k)
	}
	sort.Strings(out)
	return out
}
