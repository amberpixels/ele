package parser

// Kind is the type of a parsed pg_restore stderr event.
type Kind uint8

const (
	// KindUnknown is any line we couldn't classify. Never fatal - it is counted
	// and logged so parser drift keeps the pipeline alive rather than crashing.
	KindUnknown Kind = iota
	// KindInfo is a benign status line (connecting, entering parallel loop).
	KindInfo
	// KindDropping is a --clean DROP attempt: "dropping <desc> <tag>".
	KindDropping
	// KindCreating is an object being created: creating <desc> "<name>".
	KindCreating
	// KindProcessingData marks serial data load for a table.
	KindProcessingData
	// KindProcessingItem is a serial item being processed (carries a dump id).
	KindProcessingItem
	// KindExecuting is an item execution such as "executing SEQUENCE SET x".
	KindExecuting
	// KindLaunchItem / KindFinishItem bracket a parallel (-j) worker's item and
	// carry the dump id used to time and complete it.
	KindLaunchItem
	KindFinishItem
	// KindError is a restore error, typically "could not execute query". Its
	// DumpID (from the preceding TOC-entry context) and Command (from the
	// following "Command was:" line) are stitched in across lines.
	KindError
	// KindWarning is a pg_restore warning line.
	KindWarning
)

func (k Kind) String() string {
	switch k {
	case KindInfo:
		return "info"
	case KindDropping:
		return "dropping"
	case KindCreating:
		return "creating"
	case KindProcessingData:
		return "processing-data"
	case KindProcessingItem:
		return "processing-item"
	case KindExecuting:
		return "executing"
	case KindLaunchItem:
		return "launch-item"
	case KindFinishItem:
		return "finish-item"
	case KindError:
		return "error"
	case KindWarning:
		return "warning"
	default:
		return "unknown"
	}
}

// Event is one classified stderr line (or, for errors, a stitched group of
// lines). Only the fields relevant to a Kind are populated.
type Event struct {
	Kind Kind

	DumpID int    // TOC id for item and error events; 0 when absent
	Desc   string // object type, e.g. "TABLE", "FK CONSTRAINT"
	Name   string // object identity (creating name / processed table)
	Tag    string // object tag for drop and item events

	Message string // error/warning text
	Command string // failing SQL, from the "Command was:" line of an error

	Raw string // original line; set for KindUnknown
}
