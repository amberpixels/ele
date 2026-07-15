package render

import (
	"fmt"
	"io"
	"strings"
)

// ANSI control sequences used for in-place repainting.
const (
	ansiHideCursor = "\x1b[?25l"
	ansiShowCursor = "\x1b[?25h"
	ansiClearLine  = "\x1b[2K"
)

// Live repaints a block of lines in place on a terminal using cursor movement.
// Anything printed above the block (by the tool or a calling script) scrolls
// normally; only the block itself is rewritten. Not safe for concurrent use.
type Live struct {
	w         io.Writer
	prevLines int
	hidden    bool
}

// NewLive returns a Live writer over w (typically os.Stderr).
func NewLive(w io.Writer) *Live { return &Live{w: w} }

// Render repaints the block to match frame, clearing any lines the previous
// frame drew beyond the new one. The cursor is hidden on first paint.
func (l *Live) Render(frame []string) {
	var b strings.Builder
	if !l.hidden {
		b.WriteString(ansiHideCursor)
		l.hidden = true
	}
	if l.prevLines > 0 {
		fmt.Fprintf(&b, "\x1b[%dA", l.prevLines) // cursor up to block top
	}

	n := max(len(frame), l.prevLines)
	for i := range n {
		b.WriteString("\r")
		b.WriteString(ansiClearLine)
		if i < len(frame) {
			b.WriteString(frame[i])
		}
		b.WriteString("\n")
	}
	l.prevLines = len(frame)
	_, _ = io.WriteString(l.w, b.String())
}

// Clear erases the block, leaving the cursor at its top so a caller can print a
// final summary in its place.
func (l *Live) Clear() {
	var b strings.Builder
	if l.prevLines > 0 {
		fmt.Fprintf(&b, "\x1b[%dA", l.prevLines)
	}
	for i := range l.prevLines {
		b.WriteString("\r")
		b.WriteString(ansiClearLine)
		if i < l.prevLines-1 {
			b.WriteString("\n")
		}
	}
	if l.prevLines > 0 {
		fmt.Fprintf(&b, "\x1b[%dA", l.prevLines-1) // back to block top
	}
	l.prevLines = 0
	_, _ = io.WriteString(l.w, b.String())
}

// Close restores the cursor. Safe to call more than once.
func (l *Live) Close() {
	if l.hidden {
		_, _ = io.WriteString(l.w, ansiShowCursor)
		l.hidden = false
	}
}
