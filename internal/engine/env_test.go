package engine

import (
	"strings"
	"testing"
	"time"
)

func TestLogDestination(t *testing.T) {
	at := time.Date(2026, 7, 22, 16, 18, 15, 0, time.UTC)

	t.Run("default is timestamped in the cwd", func(t *testing.T) {
		t.Setenv(envLog, "")
		if got := logDestination(at); got != "ele-20260722-161815.log" {
			t.Errorf("logDestination = %q, want the timestamped default", got)
		}
	})

	t.Run("ELE_LOG names the path", func(t *testing.T) {
		t.Setenv(envLog, "/tmp/restore.log")
		if got := logDestination(at); got != "/tmp/restore.log" {
			t.Errorf("logDestination = %q, want /tmp/restore.log", got)
		}
	})

	// Empty has to behave as unset, the way every other var here does.
	t.Run("empty falls back to the default", func(t *testing.T) {
		t.Setenv(envLog, "")
		if got := logDestination(at); !strings.HasPrefix(got, "ele-") {
			t.Errorf("logDestination = %q, want the default", got)
		}
	})
}

func TestEnvFlags(t *testing.T) {
	t.Run("unset", func(t *testing.T) {
		t.Setenv(envPassthrough, "")
		t.Setenv(envStrictExit, "")
		if passthroughRequested() || strictExit() {
			t.Error("flags set with empty environment")
		}
	})

	t.Run("set", func(t *testing.T) {
		t.Setenv(envPassthrough, "1")
		t.Setenv(envStrictExit, "1")
		if !passthroughRequested() || !strictExit() {
			t.Error("flags not picked up")
		}
	})
}
