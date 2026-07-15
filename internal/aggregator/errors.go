package aggregator

import "strings"

// errorGroup accumulates errors that share a fingerprint template.
type errorGroup struct {
	template string          // message with quoted spans replaced by a placeholder
	sample   string          // full text of the first instance
	params   []string        // quoted spans of the first instance
	count    int             // total occurrences
	distinct map[string]bool // distinct parameter signatures, for the "N others" note
	benign   bool            // classified once, on the first instance
	seq      int             // creation order, for most-recent-first display
}

// placeholder stands in for a quoted identifier in a fingerprint template.
const placeholder = `"…"`

// fingerprint replaces every double-quoted span in msg with placeholder, giving
// a stable group template, and returns the spans it removed. Postgres quotes
// all identifiers, so blanking the quoted parts collapses "role \"a\"..." and
// "role \"b\"..." to one template while keeping a and b as sample parameters.
func fingerprint(msg string) (template string, params []string) {
	var b, cur strings.Builder
	inQuote := false
	for i := 0; i < len(msg); i++ {
		switch c := msg[i]; {
		case c == '"' && !inQuote:
			inQuote = true
			b.WriteString(placeholder)
			cur.Reset()
		case c == '"' && inQuote:
			inQuote = false
			params = append(params, cur.String())
		case inQuote:
			cur.WriteByte(c)
		default:
			b.WriteByte(c)
		}
	}
	return b.String(), params
}

// classifyBenign decides whether an error is expected noise rather than a real
// failure. The registry is deliberately small and explicit; anything it doesn't
// recognise is treated as real, so the exit code never hides a genuine problem.
func classifyBenign(msg, command string, clean, noOwner bool) bool {
	// The --clean DROP wave against a fresh database: every DROP fails with
	// "does not exist" because nothing is there yet. Classic restore noise.
	if clean && strings.Contains(msg, "does not exist") && strings.Contains(command, "DROP") {
		return true
	}
	// A dump owned by a role the target lacks (the RDS/Heroku-restored-locally
	// case), harmless when ownership isn't being applied.
	if noOwner && strings.Contains(msg, "role ") && strings.Contains(msg, "does not exist") {
		return true
	}
	return false
}
