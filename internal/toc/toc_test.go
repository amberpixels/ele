package toc

import (
	"strings"
	"testing"
)

func TestParseLine(t *testing.T) {
	tests := []struct {
		name string
		line string
		want Entry
	}{
		{
			name: "simple table",
			line: "215; 1259 16385 TABLE public activity_logs app_owner",
			want: Entry{DumpID: 215, TableOID: 1259, OID: 16385, Desc: "TABLE",
				Section: PreData, Schema: "public", Tag: "activity_logs", Owner: "app_owner"},
		},
		{
			name: "multi-word desc TABLE DATA",
			line: "4012; 0 16385 TABLE DATA public activity_logs app_owner",
			want: Entry{DumpID: 4012, TableOID: 0, OID: 16385, Desc: "TABLE DATA",
				Section: Data, Schema: "public", Tag: "activity_logs", Owner: "app_owner"},
		},
		{
			name: "three-word desc",
			line: "300; 3602 16700 TEXT SEARCH CONFIGURATION public english app_owner",
			want: Entry{DumpID: 300, TableOID: 3602, OID: 16700, Desc: "TEXT SEARCH CONFIGURATION",
				Section: PreData, Schema: "public", Tag: "english", Owner: "app_owner"},
		},
		{
			name: "tag with spaces (function signature)",
			line: "512; 1255 16500 FUNCTION public normalize(integer, text) app_owner",
			want: Entry{DumpID: 512, TableOID: 1255, OID: 16500, Desc: "FUNCTION",
				Section: PreData, Schema: "public", Tag: "normalize(integer, text)", Owner: "app_owner"},
		},
		{
			name: "post-data FK constraint",
			line: "3901; 2606 16600 FK CONSTRAINT public appointments_user_id_fkey app_owner",
			want: Entry{DumpID: 3901, TableOID: 2606, OID: 16600, Desc: "FK CONSTRAINT",
				Section: PostData, Schema: "public", Tag: "appointments_user_id_fkey", Owner: "app_owner"},
		},
		{
			// Regression: "SEQUENCE OWNED BY" must not be mis-matched as the
			// shorter known desc "SEQUENCE" (which would swallow OWNED/BY as
			// schema/tag). Real pg_dump 17 output.
			name: "SEQUENCE OWNED BY three-word desc that prefixes SEQUENCE",
			line: "3848; 0 0 SEQUENCE OWNED BY public activity_logs_id_seq postgres",
			want: Entry{DumpID: 3848, TableOID: 0, OID: 0, Desc: "SEQUENCE OWNED BY",
				Section: PreData, Schema: "public", Tag: "activity_logs_id_seq", Owner: "postgres"},
		},
		{
			// Real pg_dump table-constraint tags carry both the table and the
			// constraint name, so the tag legitimately spans two tokens.
			name: "table CONSTRAINT tag spans table + constraint name",
			line: "3688; 2606 16393 CONSTRAINT public activity_logs activity_logs_pkey postgres",
			want: Entry{DumpID: 3688, TableOID: 2606, OID: 16393, Desc: "CONSTRAINT",
				Section: PostData, Schema: "public", Tag: "activity_logs activity_logs_pkey", Owner: "postgres"},
		},
		{
			name: "pseudo-entry with dash schema and empty owner",
			line: "3450; 0 0 ENCODING - ENCODING",
			want: Entry{DumpID: 3450, TableOID: 0, OID: 0, Desc: "ENCODING",
				Section: PreData, Schema: "", Tag: "ENCODING", Owner: ""},
		},
		{
			name: "sequence set is data section",
			line: "4020; 0 0 SEQUENCE SET public activity_logs_id_seq app_owner",
			want: Entry{DumpID: 4020, TableOID: 0, OID: 0, Desc: "SEQUENCE SET",
				Section: Data, Schema: "public", Tag: "activity_logs_id_seq", Owner: "app_owner"},
		},
		{
			name: "unknown desc survives as SectionUnknown",
			line: "999; 1 2 WIDGETRY public gizmo app_owner",
			want: Entry{DumpID: 999, TableOID: 1, OID: 2, Desc: "WIDGETRY",
				Section: SectionUnknown, Schema: "public", Tag: "gizmo", Owner: "app_owner"},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, ok := parseLine(tt.line)
			if !ok {
				t.Fatalf("parseLine(%q) returned ok=false", tt.line)
			}
			if got != tt.want {
				t.Errorf("parseLine(%q)\n got: %+v\nwant: %+v", tt.line, got, tt.want)
			}
		})
	}
}

func TestParseLineRejects(t *testing.T) {
	bad := []string{
		"; no dump id",
		"not a toc line at all",
		"215; 1259", // missing desc
		"abc; 1 2 TABLE public t o",
	}
	for _, line := range bad {
		if _, ok := parseLine(line); ok {
			t.Errorf("parseLine(%q) = ok, want rejected", line)
		}
	}
}

// realistic-ish listing exercising the comment header + all three phases.
const sampleListing = `;
; Archive created at 2026-07-15 10:30:00 UTC
;     dbname: myapp
;     TOC Entries: 9
;     Format: CUSTOM
;
;
; Selected TOC Entries:
;
5; 2615 16385 SCHEMA - public app_owner
210; 1259 16400 TABLE public activity_logs app_owner
211; 1259 16410 TABLE public appointments app_owner
4012; 0 16400 TABLE DATA public activity_logs app_owner
4013; 0 16410 TABLE DATA public appointments app_owner
4020; 0 0 SEQUENCE SET public activity_logs_id_seq app_owner
3900; 2606 16500 CONSTRAINT public activity_logs_pkey app_owner
3901; 2606 16510 FK CONSTRAINT public appointments_user_id_fkey app_owner
3902; 1259 16520 INDEX public idx_appointments_date app_owner
`

func TestParsePlan(t *testing.T) {
	plan, err := Parse(strings.NewReader(sampleListing))
	if err != nil {
		t.Fatalf("Parse: %v", err)
	}
	if len(plan.Entries) != 9 {
		t.Fatalf("got %d entries, want 9", len(plan.Entries))
	}

	pre, data, post, unknown := plan.PhaseCounts()
	if pre != 3 || data != 3 || post != 3 || unknown != 0 {
		t.Errorf("PhaseCounts = pre %d / data %d / post %d / unknown %d; want 3/3/3/0",
			pre, data, post, unknown)
	}

	e, ok := plan.Get(4012)
	if !ok {
		t.Fatal("Get(4012) not found")
	}
	if e.Desc != "TABLE DATA" || e.Section != Data || e.Tag != "activity_logs" {
		t.Errorf("Get(4012) = %+v", e)
	}
}

func TestSetBytesAndDataBytes(t *testing.T) {
	plan, err := Parse(strings.NewReader(sampleListing))
	if err != nil {
		t.Fatalf("Parse: %v", err)
	}

	// Nothing sized yet.
	if total := plan.DataBytes(); total != 0 {
		t.Errorf("DataBytes before sizing = %d, want 0", total)
	}

	plan.SetBytes(4012, 1200)
	plan.SetBytes(4013, 800)
	// 4020 (SEQUENCE SET) is a data entry with no file; it contributes nothing.
	if total := plan.DataBytes(); total != 2000 {
		t.Errorf("DataBytes = %d, want 2000", total)
	}

	// SetBytes on an unknown id is a silent no-op.
	plan.SetBytes(123456, 999)
	if total := plan.DataBytes(); total != 2000 {
		t.Errorf("DataBytes after no-op = %d, want 2000", total)
	}
}
