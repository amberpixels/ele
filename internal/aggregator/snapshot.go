package aggregator

import (
	"sort"
	"time"

	"github.com/amberpixels/ele/internal/toc"
)

// PhaseProgress is one phase bar's numerator and denominator.
type PhaseProgress struct {
	Section toc.Section
	Done    int
	Total   int
}

// InFlightItem is a parallel item currently being restored.
type InFlightItem struct {
	DumpID  int
	Desc    string
	Tag     string
	Elapsed time.Duration
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

	InFlight []InFlightItem // longest-running first
	Slowest  []ItemTiming   // slowest finished items first

	Errors    []ErrorGroup // real groups first, most recent within each class
	ErrTotal  int
	ErrReal   int
	ErrBenign int

	Unknown int
}

const maxSlowest = 5

// Snapshot returns a consistent, sorted view of the current state.
func (a *Aggregator) Snapshot() Snapshot {
	s := Snapshot{
		Pre:        PhaseProgress{toc.PreData, a.done[toc.PreData], a.total[toc.PreData]},
		Data:       PhaseProgress{toc.Data, a.done[toc.Data], a.total[toc.Data]},
		Post:       PhaseProgress{toc.PostData, a.done[toc.PostData], a.total[toc.PostData]},
		ByteSized:  a.byteSized,
		BytesDone:  a.bytesDone,
		BytesTotal: a.bytesTotal,
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
