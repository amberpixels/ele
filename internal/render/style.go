package render

import (
	"io"

	"github.com/charmbracelet/lipgloss"
)

// Styles holds the lipgloss styles used to color output. Built from an output
// writer, it auto-degrades: a non-TTY, NO_COLOR, or dumb terminal yields plain
// text, so callers never branch on color themselves.
type Styles struct {
	bar     lipgloss.Style // filled progress cells (brand teal)
	empty   lipgloss.Style // unfilled progress cells
	done    lipgloss.Style // "done" marker
	pct     lipgloss.Style // percentage
	benign  lipgloss.Style // benign error rows / counts
	real    lipgloss.Style // real error rows / counts
	dim     lipgloss.Style // secondary text (log path, slowest)
	success lipgloss.Style // success outcome
	fail    lipgloss.Style // failure outcome
	skipped lipgloss.Style // "N skipped" phase annotation
	timer   lipgloss.Style // elapsed clock and current-item timer
}

// brand colors, matching the logo's teal/indigo family.
var (
	colTeal  = lipgloss.Color("#14B8A6")
	colGreen = lipgloss.Color("#22C55E")
	colRed   = lipgloss.Color("#EF4444")
	colGray  = lipgloss.Color("#6B7280")
	colAmber = lipgloss.Color("#F59E0B")
	colPink  = lipgloss.Color("#F472B6")
)

// NewStyles builds styles targeting w, detecting its color profile via lipgloss
// (truecolor / 256 / 16 / none). Pass the same writer output is rendered to.
func NewStyles(w io.Writer) *Styles {
	s := lipgloss.NewRenderer(w).NewStyle
	return &Styles{
		bar:     s().Foreground(colTeal),
		empty:   s().Foreground(colGray).Faint(true),
		done:    s().Foreground(colGreen).Bold(true),
		pct:     s().Foreground(colTeal),
		benign:  s().Faint(true),
		real:    s().Foreground(colRed).Bold(true),
		dim:     s().Faint(true),
		success: s().Foreground(colGreen).Bold(true),
		fail:    s().Foreground(colRed).Bold(true),
		skipped: s().Foreground(colAmber),
		timer:   s().Foreground(colPink),
	}
}

// plainStyles renders everything unstyled - used when no Styles are supplied
// (tests, and the frame's nil case). Zero-value lipgloss styles set no
// attributes, so Render returns its input verbatim.
func plainStyles() *Styles { return &Styles{} }
