package aggregator

import (
	"sort"
	"time"

	"github.com/amberpixels/ele/internal/toc"
)

// PhaseProgress is one phase bar's numerator and denominator. When the phase's
// restore is over (Complete), any entries pg_restore never processed count as
// Skipped rather than pending - so Done+Skipped == Total and the bar reads 100%.
type PhaseProgress struct {
	Section  toc.Section
	Done     int
	Total    int
	Skipped  int  // planned entries pg_restore skipped (known once the phase is over)
	Complete bool // the phase's restore has finished
}

// InFlightItem is a parallel item currently being restored.
type InFlightItem struct {
	DumpID  int
	Desc    string
	Tag     string
	Elapsed time.Duration
}

// WorkItem is the object a restore is currently working on, with how long it
// has been running. Serial restores have at most one; parallel (-j) restores
// map their in-flight set into a WorkItem each, longest-running first.
type WorkItem struct {
	Desc    string        // object type, e.g. "TABLE DATA", "INDEX"
	Name    string        // object identifier
	Elapsed time.Duration // time since this item started
}

// ItemTiming is a completed item's launch-to-finish duration.
type ItemTiming struct {
	DumpID int
	Desc   string
	Tag    string
	Dur    time.Duration
}

// ErrorGroup is a fingerprinted family of errors, as shown in the panel.
type ErrorGroup struct {
	Template string   // message with quoted identifiers blanked
	Sample   string   // full text of the first occurrence
	Params   []string // quoted identifiers from the first occurrence
	Count    int      // total occurrences
	Distinct int      // distinct parameter sets (for the "N others" note)
	Benign   bool
}

// Snapshot is an immutable view of restore state for the renderer.
type Snapshot struct {
	Pre  PhaseProgress
	Data PhaseProgress
	Post PhaseProgress

	ByteSized  bool
	BytesDone  int64
	BytesTotal int64

	DropCount int  // objects dropped by the --clean wave so far
	Dropping  bool // the drop wave is still running (no section has started)

	InFlight []InFlightItem // longest-running first (parallel only)
	Working  []WorkItem     // what's being restored now; drives the "working" line
	Slowest  []ItemTiming   // slowest finished items first

	Errors    []ErrorGroup // real groups first, most recent within each class
	ErrTotal  int
	ErrReal   int
	ErrBenign int

	Unknown int
}

const maxSlowest = 5

// phase builds one phase's progress. Once its restore is over, the entries
// pg_restore never processed are reported as Skipped (Done+Skipped == Total), so
// the renderer can show it as complete rather than stalled below 100%.
func (a *Aggregator) phase(s toc.Section) PhaseProgress {
	done, total := a.done[s], a.total[s]
	p := PhaseProgress{Section: s, Done: done, Total: total}
	if a.sectionDone[s] || (total > 0 && done >= total) {
		p.Complete = true
		if total > done {
			p.Skipped = total - done
		}
	}
	return p
}

// Snapshot returns a consistent, sorted view of the current state.
func (a *Aggregator) Snapshot() Snapshot {
	s := Snapshot{
		Pre:        a.phase(toc.PreData),
		Data:       a.phase(toc.Data),
		Post:       a.phase(toc.PostData),
		ByteSized:  a.byteSized,
		BytesDone:  a.bytesDone,
		BytesTotal: a.bytesTotal,
		DropCount:  a.dropCount,
		Dropping:   a.dropCount > 0 && !a.dropWaveOver,
		ErrTotal:   a.errTotal,
		Unknown:    a.unknown,
	}

	now := a.now()
	for id, inf := range a.inflight {
		s.InFlight = append(s.InFlight, InFlightItem{
			DumpID: id, Desc: inf.desc, Tag: inf.tag, Elapsed: now.Sub(inf.start),
		})
	}
	sort.Slice(s.InFlight, func(i, j int) bool {
		return s.InFlight[i].Elapsed > s.InFlight[j].Elapsed
	})

	// The "working" line draws from one unified list. Under -j it's the inflight
	// items (already sorted longest-first); serially it's the single current item.
	switch {
	case len(s.InFlight) > 0:
		for _, it := range s.InFlight {
			s.Working = append(s.Working, WorkItem{Desc: it.Desc, Name: it.Tag, Elapsed: it.Elapsed})
		}
	case a.hasCurrent:
		s.Working = []WorkItem{{Desc: a.curDesc, Name: a.curName, Elapsed: now.Sub(a.curStart)}}
	}

	s.Slowest = append(s.Slowest, a.timings...)
	sort.Slice(s.Slowest, func(i, j int) bool { return s.Slowest[i].Dur > s.Slowest[j].Dur })
	if len(s.Slowest) > maxSlowest {
		s.Slowest = s.Slowest[:maxSlowest]
	}

	s.Errors = a.errorGroups()
	for _, g := range s.Errors {
		if g.Benign {
			s.ErrBenign += g.Count
		} else {
			s.ErrReal += g.Count
		}
	}
	return s
}

// errorGroups returns the groups ordered real-first, then most-recent-first
// within each class - the order the panel and counter line want.
func (a *Aggregator) errorGroups() []ErrorGroup {
	out := make([]ErrorGroup, 0, len(a.groups))
	for _, g := range a.groups {
		out = append(out, ErrorGroup{
			Template: g.template,
			Sample:   g.sample,
			Params:   g.params,
			Count:    g.count,
			Distinct: len(g.distinct),
			Benign:   g.benign,
		})
	}
	// Recover recency from creation sequence, kept alongside each group.
	seq := map[string]int{}
	for _, g := range a.groups {
		seq[g.template] = g.seq
	}
	sort.Slice(out, func(i, j int) bool {
		if out[i].Benign != out[j].Benign {
			return !out[i].Benign // real groups first
		}
		return seq[out[i].Template] > seq[out[j].Template] // most recent first
	})
	return out
}
