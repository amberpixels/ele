package engine

import (
	"os"
	"time"
)

// ele has no flags of its own - pg_restore owns the whole argv - so every knob
// is an environment variable. These three are the engine's; the renderer reads
// ELE_PLAIN / NO_COLOR / CI / CLAUDECODE in internal/render, and --replay reads
// ELE_REPLAY_SECONDS. Unset and empty always mean the same thing.
const (
	envLog         = "ELE_LOG"
	envPassthrough = "ELE_PASSTHROUGH"
	envStrictExit  = "ELE_STRICT_EXIT"
)

// logDestination is where the raw pg_restore stderr gets teed: ELE_LOG when it
// names a path, else a timestamped file in the working directory.
func logDestination(now time.Time) string {
	if p := os.Getenv(envLog); p != "" {
		return p
	}
	return "ele-" + now.Format("20060102-150405") + ".log"
}

// passthroughRequested reports whether ELE_PASSTHROUGH asks for the raw
// firehose - pg_restore's own stderr and exit code, no aggregation. It is the
// only way back to unwrapped output; the degraded modes never give it.
func passthroughRequested() bool { return os.Getenv(envPassthrough) != "" }

// strictExit reports whether ELE_STRICT_EXIT asks for pg_restore's raw exit
// code, leaving a benign-only failure nonzero.
func strictExit() bool { return os.Getenv(envStrictExit) != "" }
