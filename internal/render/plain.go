package render

import (
	"fmt"
	"io"
	"strconv"
	"time"

	"github.com/amberpixels/ele/internal/aggregator"
	"github.com/amberpixels/years"
)

// PlainProgress emits a one-line status to w at meaningful milestones - each
// phase completion and every 10% within a phase, or, with no denominators to
// measure against, every countlessStep objects. It is the degraded-mode
// substitute for the live repaint block (non-TTY, CI, NO_COLOR, ELE_PLAIN),
// never the raw firehose. Feed it snapshots on a ticker.
// countlessStep and countlessInterval pace the line when there are no
// denominators to hit deciles against: a milestone is every N objects, or every
// N seconds, whichever lands first. Without them a countless restore would
// print one line at the start and then nothing for the rest of the run.
const (
	countlessStep     = 50
	countlessInterval = 15 * time.Second
)

type PlainProgress struct {
	w  io.Writer
	st *Styles

	started    bool
	preDone    bool
	dataDecile int
	postDecile int

	// countless pacing: the object count and the elapsed reading at the last
	// emitted line.
	lastCount int
	lastEmit  time.Duration
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

	if s.Countless {
		done := s.Pre.Done + s.Data.Done + s.Post.Done
		if done-p.lastCount >= countlessStep || elapsed-p.lastEmit >= countlessInterval {
			milestone = true
		}
		if milestone {
			p.lastCount, p.lastEmit = done, elapsed
			p.emit(s, elapsed)
		}
		return
	}

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
		countOr(s.Countless, s.Pre.Done, frac(s.Pre.Done, s.Pre.Total)),
		countOr(s.Countless, s.Data.Done, fracPct(s.Data.Done, s.Data.Total)),
		countOr(s.Countless, s.Post.Done, fracPct(s.Post.Done, s.Post.Total)),
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

// countOr is the bare count in countless mode, where "0/0 0%" would be a lie
// dressed as a fraction, and the given fraction otherwise.
func countOr(countless bool, done int, fraction string) string {
	if countless {
		return strconv.Itoa(done)
	}
	return fraction
}

func frac(done, total int) string { return fmt.Sprintf("%d/%d", done, total) }

func fracPct(done, total int) string {
	if total <= 0 {
		return frac(done, total)
	}
	return fmt.Sprintf("%d/%d %d%%", done, total, done*100/total)
}
