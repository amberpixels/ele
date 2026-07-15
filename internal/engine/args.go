package engine

import "strings"

// flags are the pg_restore argv facts ele needs: whether it's a real restore
// (-d/--dbname), and which options steer benign classification.
type flags struct {
	clean   bool
	noOwner bool
	hasDB   bool
	dbName  string
}

// detectFlags scans pg_restore argv for the options ele cares about. It does
// not fully parse pg_restore's flag surface - only the handful that matter.
func detectFlags(args []string) flags {
	var f flags
	for i, a := range args {
		switch {
		case a == "-c" || a == "--clean":
			f.clean = true
		case a == "-O" || a == "--no-owner":
			f.noOwner = true
		case a == "-d" || a == "--dbname":
			f.hasDB = true
			if i+1 < len(args) {
				f.dbName = args[i+1]
			}
		case strings.HasPrefix(a, "--dbname="):
			f.hasDB = true
			f.dbName = strings.TrimPrefix(a, "--dbname=")
		}
	}
	return f
}

// findDumpPath returns the archive argument - the last positional token that
// exists on disk. Option values (a db name, host, job count) don't stat as
// files, so they're skipped naturally. exists is injected for testing.
func findDumpPath(args []string, exists func(string) bool) string {
	found := ""
	for _, a := range args {
		if strings.HasPrefix(a, "-") {
			continue
		}
		if exists(a) {
			found = a
		}
	}
	return found
}
