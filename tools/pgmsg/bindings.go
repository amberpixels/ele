package main

// binding is one pg_dump message the parser acts on: the upstream msgid, the
// event it means, and how its arguments are read back out of the printed line.
//
// This table is hand-maintained on purpose. A gettext catalog carries wording,
// never meaning, so no generator can decide that "launching item %d %s %s" is
// the start of a parallel item. What the generator does instead is prove every
// line below still exists upstream, and emit the literal text to match on.
type binding struct {
	msgid string
	kind  string // parser.Kind constant
	shape string // parser shape constant: which fields the arguments fill
}

// bindings covers exactly what parser.classify matches on a message body.
// The envelope around it - the "pg_restore:" prefix, the error:/warning:/detail:
// severity tags, the "Command was:" continuation - comes from
// src/common/logging.c rather than pg_dump's catalog, and stays hand-written in
// the parser.
var bindings = []binding{
	{"connecting to database for restore", "KindInfo", "shapeNone"},
	{"entering main parallel loop", "KindInfo", "shapeNone"},
	{"finished main parallel loop", "KindInfo", "shapeNone"},
	{"while INITIALIZING:", "KindInfo", "shapeNone"},
	{"while PROCESSING TOC:", "KindInfo", "shapeNone"},
	{"while FINALIZING:", "KindInfo", "shapeNone"},

	// Context for the error that follows it; sets parser state, emits nothing.
	{"from TOC entry %d; %u %u %s %s %s", "KindUnknown", "shapeTOCEntry"},

	{"dropping %s %s", "KindDropping", "shapeDescTag"},
	{"creating %s \"%s.%s\"", "KindCreating", "shapeDescQuoted"},
	{"creating %s \"%s\"", "KindCreating", "shapeDescQuoted"},
	{"processing data for table \"%s.%s\"", "KindProcessingData", "shapeQuotedName"},
	{"processing item %d %s %s", "KindProcessingItem", "shapeItem"},
	{"launching item %d %s %s", "KindLaunchItem", "shapeItem"},
	{"finished item %d %s %s", "KindFinishItem", "shapeItem"},
	{"executing %s %s", "KindExecuting", "shapeDescName"},
	{"executing %s", "KindExecuting", "shapeDescName"},
}
