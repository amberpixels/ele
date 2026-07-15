package render

import (
	"fmt"
	"io"

	"github.com/amberpixels/ele/internal/aggregator"
)

// PlainProgress emits a one-line status to w at meaningful milestones - each
// phase completion and every 10% within a phase. It is the degraded-mode
// substitute for the live repaint block (non-TTY, CI, NO_COLOR, ELE_PLAIN),
// never the raw firehose. Feed it snapshots on a ticker.
type PlainProgress struct {
	w  io.Writer
	st *Styles

	started    bool
	preDone    bool
	dataDecile int
	postDecile int
}

// NewPlainProgress returns an emitter writing to w.
func NewPlainProgress(w io.Writer) *PlainProgress {
	return &PlainProgress{w: w, st: NewStyles(w), dataDecile: -1, postDecile: -1}
}

// Update prints a status line if a milestone was reached since the last call.
func (p *PlainProgress) Update(s aggregator.Snapshot) {
	milestone := !p.started
	p.started = true

	if s.Pre.Total > 0 && s.Pre.Done == s.Pre.Total && !p.preDone {
		p.preDone = true
		milestone = true
	}
	if d := decile(s.Data.Done, s.Data.Total); d != p.dataDecile {
		p.dataDecile = d
		milestone = true
	}
	if d := decile(s.Post.Done, s.Post.Total); d != p.postDecile {
		p.postDecile = d
		milestone = true
	}
	if milestone {
		p.emit(s)
	}
}

func (p *PlainProgress) emit(s aggregator.Snapshot) {
	realStyle := p.st.dim
	if s.ErrReal > 0 {
		realStyle = p.st.real
	}
	fmt.Fprintf(p.w, "ele · pre %s · data %s · post %s · %d err · %s\n",
		frac(s.Pre.Done, s.Pre.Total),
		fracPct(s.Data.Done, s.Data.Total),
		fracPct(s.Post.Done, s.Post.Total),
		s.ErrTotal,
		realStyle.Render(fmt.Sprintf("%d real", s.ErrReal)),
	)
}

func decile(done, total int) int {
	if total <= 0 {
		return 0
	}
	return done * 10 / total
}

func frac(done, total int) string { return fmt.Sprintf("%d/%d", done, total) }

func fracPct(done, total int) string {
	if total <= 0 {
		return frac(done, total)
	}
	return fmt.Sprintf("%d/%d %d%%", done, total, done*100/total)
}
