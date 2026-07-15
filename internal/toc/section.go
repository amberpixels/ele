package toc

// Section is one of pg_dump's three restore phases, replayed strictly in
// order: pre-data DDL, then table data, then post-data objects (indexes,
// constraints, triggers) that are cheaper to build once rows are present.
type Section uint8

const (
	// SectionUnknown covers object types we don't recognise. It sorts first so
	// unknown entries never inflate a real phase's denominator.
	SectionUnknown Section = iota
	PreData
	Data
	PostData
)

func (s Section) String() string {
	switch s {
	case PreData:
		return "pre-data"
	case Data:
		return "data"
	case PostData:
		return "post-data"
	default:
		return "unknown"
	}
}

// descSection maps a pg_dump entry description to its restore section. The
// section is not printed in `pg_restore -l`, so we reconstruct it the way
// pg_dump assigns it internally (PG13-18). Anything not listed is treated as
// SectionUnknown rather than guessed.
var descSection = map[string]Section{
	// --- pseudo / archive-control entries (emitted with pre-data) ---
	"ENCODING":            PreData,
	"STDSTRINGS":          PreData,
	"SEARCHPATH":          PreData,
	"DATABASE":            PreData,
	"DATABASE PROPERTIES": PreData,

	// --- pre-data: DDL that must exist before rows load ---
	"SCHEMA":                    PreData,
	"EXTENSION":                 PreData,
	"SHELL TYPE":                PreData,
	"TYPE":                      PreData,
	"DOMAIN":                    PreData,
	"FUNCTION":                  PreData,
	"PROCEDURE":                 PreData,
	"AGGREGATE":                 PreData,
	"OPERATOR":                  PreData,
	"OPERATOR CLASS":            PreData,
	"OPERATOR FAMILY":           PreData,
	"COLLATION":                 PreData,
	"CONVERSION":                PreData,
	"CAST":                      PreData,
	"TRANSFORM":                 PreData,
	"PROCEDURAL LANGUAGE":       PreData,
	"TABLE":                     PreData,
	"TABLE ATTACH":              PreData, // partition attach happens with the DDL
	"SEQUENCE":                  PreData,
	"SEQUENCE OWNED BY":         PreData, // 3-word desc; without it "SEQUENCE" would mis-match
	"VIEW":                      PreData,
	"MATERIALIZED VIEW":         PreData, // created WITH NO DATA; refreshed post-data
	"FOREIGN TABLE":             PreData,
	"TABLESPACE":                PreData,
	"DEFAULT":                   PreData, // column DEFAULT expression
	"TEXT SEARCH PARSER":        PreData,
	"TEXT SEARCH DICTIONARY":    PreData,
	"TEXT SEARCH TEMPLATE":      PreData,
	"TEXT SEARCH CONFIGURATION": PreData,
	"FOREIGN DATA WRAPPER":      PreData,
	"SERVER":                    PreData,
	"USER MAPPING":              PreData,
	"PUBLICATION":               PreData,
	"SUBSCRIPTION":              PreData,

	// --- data: the row-bearing phase ---
	"TABLE DATA":    Data,
	"SEQUENCE SET":  Data,
	"BLOBS":         Data,
	"BLOB DATA":     Data,
	"LARGE OBJECTS": Data,

	// --- post-data: cheaper to build once rows are present ---
	"CONSTRAINT":             PostData, // PRIMARY KEY / UNIQUE / EXCLUDE
	"CHECK CONSTRAINT":       PostData,
	"FK CONSTRAINT":          PostData,
	"INDEX":                  PostData,
	"INDEX ATTACH":           PostData,
	"TRIGGER":                PostData,
	"RULE":                   PostData,
	"POLICY":                 PostData,
	"ROW SECURITY":           PostData,
	"EVENT TRIGGER":          PostData,
	"STATISTICS":             PostData, // CREATE STATISTICS (extended stats)
	"STATISTICS DATA":        PostData, // pg_statistic_ext_data, PG15+
	"PUBLICATION TABLE":      PostData,
	"MATERIALIZED VIEW DATA": PostData, // REFRESH MATERIALIZED VIEW

	// --- attach-to-parent entries (SECTION_NONE); cluster at end, closest to post-data ---
	"COMMENT":        PostData,
	"SECURITY LABEL": PostData,
	"ACL":            PostData,
	"DEFAULT ACL":    PostData,
}

// maxDescWords is the most words in any descSection key ("TEXT SEARCH
// CONFIGURATION"); bounds the parser's longest-match scan.
const maxDescWords = 3
