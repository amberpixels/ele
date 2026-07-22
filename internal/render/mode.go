package render

import (
	"os"

	"golang.org/x/term"
)

// Plain reports whether output should be plain (no repaint block): when stderr
// is not a terminal, or the environment asks for it (NO_COLOR, CI, CLAUDECODE,
// ELE_PLAIN). The live repaint is reserved for interactive terminals.
func Plain(stderr *os.File) bool {
	for _, k := range []string{"ELE_PLAIN", "NO_COLOR", "CI", "CLAUDECODE"} {
		if os.Getenv(k) != "" {
			return true
		}
	}
	return !term.IsTerminal(int(stderr.Fd()))
}

// defaultWidth is the assumed width when stderr can't be measured.
const defaultWidth = 100

// Size returns the terminal width and height for stderr. Width falls back to a
// sane default when it isn't a terminal or can't be measured; height is 0 then,
// meaning "unknown" - callers must not cap anything to it.
func Size(stderr *os.File) (width, height int) {
	if w, h, err := term.GetSize(int(stderr.Fd())); err == nil && w > 0 {
		return w, h
	}
	return defaultWidth, 0
}

// Width returns the terminal width for stderr, or a sane default when it isn't
// a terminal or can't be measured.
func Width(stderr *os.File) int {
	w, _ := Size(stderr)
	return w
}
