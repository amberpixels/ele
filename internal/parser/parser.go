// Package parser turns pg_restore --verbose stderr into a stream of typed
// Events. It is resilient to two realities of that stream: the `pg_restore: `
// envelope prefix, and the way parallel (-j) workers write the same fd without
// locking, which doubles, splits, or drops that prefix mid-line. Lines it can't
// classify become KindUnknown rather than errors, so wording drift across
// PostgreSQL versions never breaks the pipeline.
//
// The wordings it matches on are not hand-written: messages_gen.go is generated
// from pg_dump's own gettext catalogs across the supported releases (see
// tools/pgmsg), and messages.go binds each one to the Event it means.
package parser

import (
	"strconv"
	"strings"

	"github.com/amberpixels/ele/internal/toc"
)

const prefix = "pg_restore:"

// Parser translates stderr lines into Events. Most lines map to a single event,
// but errors span up to three lines - a "from TOC entry" context line, the
// "error:" line, and a following "Command was:" line - so the Parser holds a
// little state to stitch them. It is not safe for concurrent use.
type Parser struct {
	pendingCtxID int    // dump id from the last "from TOC entry" line, for the next error
	pendingErr   *Event // an error awaiting its "Command was:" line
}

// New returns a ready Parser.
func New() *Parser { return &Parser{} }

// Feed classifies one raw stderr line and returns the events it produced (often
// one, sometimes zero while an error is still being assembled, occasionally two
// when a previous error flushes).
func (p *Parser) Feed(line string) []Event {
	body := stripPrefix(line)

	// Continuation lines attach to an in-flight error and emit nothing yet.
	if cmd, ok := cut(body, "Command was:"); ok {
		if p.pendingErr != nil {
			p.pendingErr.Command = strings.TrimSpace(cmd)
			return p.flushErr()
		}
		return nil
	}
	if _, ok := cut(body, "detail:"); ok {
		return nil // detail lines add nothing we use yet
	}

	// Any other line means the previous error (if any) is complete.
	var out []Event
	out = append(out, p.flushErr()...)

	if ev, ok := p.classify(body, line); ok {
		if ev.Kind == KindError {
			p.pendingErr = &ev // hold for a possible "Command was:"
			return out
		}
		out = append(out, ev)
	}
	return out
}

// Flush emits any error still awaiting its Command line (call at end of stream).
func (p *Parser) Flush() []Event { return p.flushErr() }

func (p *Parser) flushErr() []Event {
	if p.pendingErr == nil {
		return nil
	}
	ev := *p.pendingErr
	p.pendingErr = nil
	return []Event{ev}
}

// classify maps a de-prefixed body to an Event, matching it against the
// generated message table (messages_gen.go). raw is the original line, kept for
// the KindUnknown fallback. ok is false only for context-only lines that produce
// no event of their own (the "from TOC entry" line).
func (p *Parser) classify(body, raw string) (Event, bool) {
	// Severity tags are part of the envelope - they come from
	// src/common/logging.c, not pg_dump's message catalog - so they're matched
	// here rather than in the generated table.
	if msg, ok := cut(body, "error: "); ok {
		ev := Event{Kind: KindError, Message: msg, DumpID: p.pendingCtxID}
		p.pendingCtxID = 0
		return ev, true
	}
	if msg, ok := cut(body, "warning: "); ok {
		return Event{Kind: KindWarning, Message: msg}, true
	}

	m, args, ok := matchMessage(body)
	if !ok {
		return Event{Kind: KindUnknown, Raw: raw}, true
	}

	switch m.shape {
	case shapeTOCEntry:
		// Error context, not an event of its own: the remainder is a TOC
		// listing line, so reuse the toc parser for its id.
		if e, ok := toc.ParseListingLine(args); ok {
			p.pendingCtxID = e.DumpID
		}
		return Event{}, false

	case shapeDescTag:
		desc, tag := splitDescTag(args)
		return Event{Kind: m.kind, Desc: desc, Tag: tag}, true

	case shapeDescName:
		desc, name := splitDescTag(args)
		return Event{Kind: m.kind, Desc: desc, Name: name}, true

	case shapeDescQuoted:
		desc, name := splitDescQuoted(args)
		return Event{Kind: m.kind, Desc: desc, Name: name}, true

	case shapeQuotedName:
		return Event{Kind: m.kind, Name: unquote(args)}, true

	case shapeItem:
		id, desc, tag := splitItem(args)
		return Event{Kind: m.kind, DumpID: id, Desc: desc, Tag: tag}, true

	default: // shapeNone: the message is its own meaning
		return Event{Kind: m.kind, Raw: body}, true
	}
}

// stripPrefix removes any run of "pg_restore:" envelope prefixes from the front
// of a line, tolerating the doubling ("pg_restore: pg_restore: "), missing
// spaces ("pg_restore:pg_restore:"), and prefix-less remnants that -j worker
// interleaving produces. The result is the message body, left-trimmed.
func stripPrefix(line string) string {
	for {
		line = strings.TrimLeft(line, " ")
		if strings.HasPrefix(line, prefix) {
			line = line[len(prefix):]
			continue
		}
		return line
	}
}

// cut returns the text after tag and true if body starts with tag.
func cut(body, tag string) (string, bool) {
	if strings.HasPrefix(body, tag) {
		return body[len(tag):], true
	}
	return "", false
}

// splitItem parses "<id> <desc...> <tag...>" from an item line. desc is matched
// against the known TOC descriptions (it may be multi-word, e.g. "TABLE DATA");
// the remainder is the tag.
func splitItem(s string) (id int, desc, tag string) {
	fields := strings.Fields(s)
	if len(fields) == 0 {
		return 0, "", ""
	}
	id, _ = strconv.Atoi(fields[0])
	desc, n := toc.MatchDesc(fields[1:])
	tag = strings.Join(fields[1+n:], " ")
	return id, desc, tag
}

// splitDescTag parses "<desc...> <rest...>" where desc is a known multi-word
// TOC description and rest is whatever follows (a tag or object name).
func splitDescTag(s string) (desc, rest string) {
	fields := strings.Fields(s)
	if len(fields) == 0 {
		return "", ""
	}
	desc, n := toc.MatchDesc(fields)
	return desc, strings.Join(fields[n:], " ")
}

// splitDescQuoted parses `<desc...> "<name>"` from a creating line, returning
// the description and the unquoted name.
func splitDescQuoted(s string) (desc, name string) {
	q := strings.IndexByte(s, '"')
	if q < 0 {
		return strings.TrimSpace(s), ""
	}
	return strings.TrimSpace(s[:q]), unquote(s[q:])
}

// unquote returns the contents of the first double-quoted span in s, or s
// trimmed if it isn't quoted.
func unquote(s string) string {
	s = strings.TrimSpace(s)
	if len(s) >= 2 && s[0] == '"' {
		if end := strings.IndexByte(s[1:], '"'); end >= 0 {
			return s[1 : 1+end]
		}
	}
	return s
}
