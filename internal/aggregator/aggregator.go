// Package aggregator is the one stateful stage of the pipeline. It consumes the
// parser's event stream against the preflight RestorePlan and maintains live
// restore state: per-phase progress, in-flight parallel items, item timings,
// grouped-and-classified errors, and an unknown-line count. Snapshot exposes an
// immutable view for the renderer.
package aggregator

import (
	"strings"
	"time"

	"github.com/amberpixels/ele/internal/parser"
	"github.com/amberpixels/ele/internal/toc"
)

// Config carries the restore flags that affect classification.
type Config struct {
	Clean   bool // --clean present: does-not-exist DROP noise is benign
	NoOwner bool // --no-owner present: missing-role errors are benign
}

// Aggregator accumulates restore state. It is fed one event at a time and is
// not safe for concurrent use; call Snapshot to read a consistent view.
type Aggregator struct {
	plan *toc.RestorePlan
	cfg  Config
	now  func() time.Time // injectable clock for item timing

	// phase denominators, from the plan
	total map[toc.Section]int
	// phase progress
	done map[toc.Section]int
	// sections whose restore is over: their unprocessed (total-done) entries were
	// skipped by pg_restore, not still pending. Set as a later section starts
	// (serial, where order is strict) and for every section at Finish.
	sectionDone map[toc.Section]bool
	doneIDs     map[int]bool // entries already counted, keyed by dump id
	bytesTotal  int64
	bytesDone   int64
	byteSized   bool

	// --clean DROP wave: pg_restore drops every existing object before restoring.
	// It precedes all three sections and has no denominator, so we just count it.
	dropCount    int
	dropWaveOver bool // flips once real restore progress begins

	// parallel (-j) is inferred from the presence of id-bearing item events;
	// in serial mode we count id-less creating/processing lines instead.
	parallel bool
	inflight map[int]*inflightItem

	// current serial item: the object named by the most recent creating /
	// processing-data / executing line, with the time it started. In parallel
	// mode "current" is the inflight set instead, so these stay unset.
	curDesc, curName string
	curStart         time.Time
	hasCurrent       bool

	timings []ItemTiming

	groups   map[string]*errorGroup
	groupSeq int
	errTotal int

	unknown int
}

type inflightItem struct {
	desc, tag string
	start     time.Time
}

// New builds an Aggregator for a restore plan and config.
func New(plan *toc.RestorePlan, cfg Config) *Aggregator {
	pre, data, post, _ := plan.PhaseCounts()
	bytesTotal, byteSized := plan.DataBytes(), false
	// DataBytes is only meaningful when preflight sized every data file; the
	// caller signals that via a nonzero total together with ByteSized. Here we
	// treat any positive total as byte-capable and let the renderer decide.
	if bytesTotal > 0 {
		byteSized = true
	}
	return &Aggregator{
		plan: plan,
		cfg:  cfg,
		now:  time.Now,
		total: map[toc.Section]int{
			toc.PreData: pre, toc.Data: data, toc.PostData: post,
		},
		done:        map[toc.Section]int{},
		sectionDone: map[toc.Section]bool{},
		doneIDs:     map[int]bool{},
		bytesTotal:  bytesTotal,
		byteSized:   byteSized,
		inflight:    map[int]*inflightItem{},
		groups:      map[string]*errorGroup{},
	}
}

// Feed folds one event into the running state.
func (a *Aggregator) Feed(ev parser.Event) {
	switch ev.Kind {
	case parser.KindProcessingItem:
		a.parallel = true
		a.completeID(ev.DumpID)

	case parser.KindLaunchItem:
		a.parallel = true
		a.inflight[ev.DumpID] = &inflightItem{desc: ev.Desc, tag: ev.Tag, start: a.now()}

	case parser.KindFinishItem:
		a.parallel = true
		if inf, ok := a.inflight[ev.DumpID]; ok {
			a.timings = append(a.timings, ItemTiming{
				DumpID: ev.DumpID, Desc: inf.desc, Tag: inf.tag, Dur: a.now().Sub(inf.start),
			})
			delete(a.inflight, ev.DumpID)
		}
		a.completeID(ev.DumpID)

	case parser.KindCreating:
		if !a.parallel {
			a.setCurrent(ev.Desc, ev.Name)
			a.completeSerial(toc.SectionOf(ev.Desc))
		}
	case parser.KindProcessingData:
		if !a.parallel {
			a.setCurrent("TABLE DATA", ev.Name)
			a.completeSerial(toc.Data)
		}
	case parser.KindExecuting:
		if !a.parallel {
			a.setCurrent(ev.Desc, ev.Name)
			a.completeSerial(toc.SectionOf(ev.Desc)) // SEQUENCE SET -> data
		}

	case parser.KindDropping:
		// The --clean DROP wave runs before any section: pg_restore drops every
		// existing object first. Count it for the cleanup line; it never moves a
		// phase bar (only creates do), and it ends once real restore progress
		// begins.
		if !a.dropWaveOver {
			a.dropCount++
		}

	case parser.KindError:
		a.recordError(ev)

	case parser.KindUnknown:
		a.unknown++
	}
}

// completeID marks the entry with the given dump id done, once. Its phase and
// byte size come from the plan. Ids not in the plan are ignored.
func (a *Aggregator) completeID(id int) {
	if a.doneIDs[id] {
		return
	}
	e, ok := a.plan.Get(id)
	if !ok {
		return
	}
	a.doneIDs[id] = true
	a.incPhase(e.Section)
	if e.Section == toc.Data && e.HasBytes {
		a.bytesDone += e.Bytes
	}
}

// completeSerial advances a phase in serial mode, where events carry no dump id
// but each id-less completion line maps one-to-one to a distinct entry.
//
// It also resolves pre-data early: pg_restore loads data only after every
// pre-data object is created, so the first data event means pre-data creation is
// over and its remainder was skipped. We deliberately don't infer the
// data->post-data boundary the same way - COMMENT entries are post-data yet
// pg_restore emits them among the early creates, so section order isn't strict.
// Only Finish resolves data and post-data.
func (a *Aggregator) completeSerial(s toc.Section) {
	a.incPhase(s)
	if s == toc.Data {
		a.sectionDone[toc.PreData] = true
	}
}

// Finish records that the restore is over: every phase's unprocessed entries are
// now known to be skipped rather than pending. Call once after the stream ends
// and the process exited normally (not on a signal/abort, where the remainder is
// genuinely incomplete).
func (a *Aggregator) Finish() {
	a.sectionDone[toc.PreData] = true
	a.sectionDone[toc.Data] = true
	a.sectionDone[toc.PostData] = true
}

// setCurrent records the object a serial restore is now working on, restarting
// its timer. pg_restore emits the "creating"/"processing data" line as it starts
// the item, so the elapsed time this yields is time-on-current-item - exactly
// what tells a stalled-looking restore apart from a long single-table load.
func (a *Aggregator) setCurrent(desc, name string) {
	a.curDesc, a.curName, a.curStart, a.hasCurrent = desc, name, a.now(), true
}

// incPhase increments a phase's done count, capped at its total so a stray
// double-count can never push a bar past 100%. The first real completion also
// ends the DROP wave.
func (a *Aggregator) incPhase(s toc.Section) {
	a.dropWaveOver = true
	if a.done[s] < a.total[s] {
		a.done[s]++
	}
}

func (a *Aggregator) recordError(ev parser.Event) {
	a.errTotal++
	tmpl, params := fingerprint(ev.Message)
	g := a.groups[tmpl]
	if g == nil {
		g = &errorGroup{
			template: tmpl,
			sample:   ev.Message,
			params:   params,
			distinct: map[string]bool{},
			benign:   classifyBenign(ev.Message, ev.Command, a.cfg.Clean, a.cfg.NoOwner),
			seq:      a.groupSeq,
		}
		a.groups[tmpl] = g
		a.groupSeq++
	}
	g.count++
	g.distinct[strings.Join(params, "\x00")] = true
}
