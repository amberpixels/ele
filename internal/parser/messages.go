package parser

import "strings"

// A message body is matched against a table generated from pg_dump's own
// gettext catalogs - see tools/pgmsg and messages_gen.go. This file is the
// hand-written half of that split: what a matched message means, and which
// Event fields its arguments fill. Wording lives upstream; meaning lives here.

// shape is how a message's format arguments are read back out of the printed
// line. Each shape is named for the Event fields it fills.
type shape uint8

const (
	shapeNone       shape = iota // no arguments worth keeping
	shapeTOCEntry                // a TOC listing line; sets the error-context dump id
	shapeDescTag                 // "<desc...> <tag...>" -> Desc, Tag
	shapeDescName                // "<desc...> <name...>" -> Desc, Name
	shapeDescQuoted              // `<desc...> "<name>"` -> Desc, Name
	shapeQuotedName              // `"<name>"` -> Name
	shapeItem                    // "<id> <desc...> <tag...>" -> DumpID, Desc, Tag
)

// message is one row of the generated table: the text pg_restore prints before
// its first argument, and what that message means.
type message struct {
	literal string
	exact   bool // the whole message is literal - it takes no arguments
	kind    Kind
	shape   shape
}

// matchMessage finds the message whose literal opens body and returns it with
// the remainder: the arguments pg_restore filled in. The table is ordered
// longest-literal-first, so the first hit is the most specific one.
func matchMessage(body string) (message, string, bool) {
	for _, m := range messages {
		if m.exact {
			if body == m.literal {
				return m, "", true
			}
			continue
		}
		if strings.HasPrefix(body, m.literal) {
			return m, body[len(m.literal):], true
		}
	}
	return message{}, "", false
}
