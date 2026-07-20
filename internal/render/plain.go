package render

import (
	"fmt"
	"io"
	"time"

	"github.com/amberpixels/ele/internal/aggregator"
	"github.com/amberpixels/years"
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
// elapsed is the wall time since the restore started; 0 omits it.
func (p *PlainProgress) Update(s aggregator.Snapshot, elapsed time.Duration) {
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
		p.emit(s, elapsed)
	}
}

func (p *PlainProgress) emit(s aggregator.Snapshot, elapsed time.Duration) {
	realStyle := p.st.dim
	if s.ErrReal > 0 {
		realStyle = p.st.real
	}
	fmt.Fprintf(p.w, "ele · pre %s · data %s · post %s · %d err · %s",
		frac(s.Pre.Done, s.Pre.Total),
		fracPct(s.Data.Done, s.Data.Total),
		fracPct(s.Post.Done, s.Post.Total),
		s.ErrTotal,
		realStyle.Render(fmt.Sprintf("%d real", s.ErrReal)),
	)
	if len(s.Working) > 0 {
		fmt.Fprintf(p.w, " · %s", p.st.dim.Render(workingDesc(s.Working[0])))
	}
	if elapsed > 0 {
		fmt.Fprintf(p.w, " · %s", p.st.dim.Render(years.FormatDurationClock(elapsed)))
	}
	fmt.Fprintln(p.w)
}

// workingDesc is the compact "TYPE name" label for the current object.
func workingDesc(it aggregator.WorkItem) string {
	if it.Desc == "" {
		return it.Name
	}
	return it.Desc + " " + it.Name
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
