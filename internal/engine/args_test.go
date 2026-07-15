package engine

import "testing"

func TestDetectFlags(t *testing.T) {
	tests := []struct {
		name           string
		args           []string
		clean, noOwner bool
		hasDB          bool
		dbName         string
	}{
		{"typical restore", []string{"-d", "throwaway", "--clean", "--no-owner", "x.dump"}, true, true, true, "throwaway"},
		{"short flags", []string{"-c", "-O", "-d", "mydb", "x.dump"}, true, true, true, "mydb"},
		{"dbname equals form", []string{"--dbname=prod", "x.dump"}, false, false, true, "prod"},
		{"no database (script out)", []string{"-f", "out.sql", "x.dump"}, false, false, false, ""},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			f := detectFlags(tt.args)
			if f.clean != tt.clean || f.noOwner != tt.noOwner || f.hasDB != tt.hasDB || f.dbName != tt.dbName {
				t.Errorf("detectFlags(%v) = %+v", tt.args, f)
			}
		})
	}
}

func TestFindDumpPath(t *testing.T) {
	// "throwaway" and "4" look like positionals but aren't files; only the dump is.
	exists := func(p string) bool { return p == "/dumps/app.dump" }
	args := []string{"-d", "throwaway", "-j", "4", "--clean", "/dumps/app.dump"}
	if got := findDumpPath(args, exists); got != "/dumps/app.dump" {
		t.Errorf("findDumpPath = %q, want /dumps/app.dump", got)
	}

	// Nothing on disk -> empty (stdin restore).
	if got := findDumpPath([]string{"-d", "db"}, func(string) bool { return false }); got != "" {
		t.Errorf("findDumpPath = %q, want empty", got)
	}
}
