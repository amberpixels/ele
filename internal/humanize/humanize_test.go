package humanize

import (
	"testing"
	"time"
)

func TestDuration(t *testing.T) {
	cases := []struct {
		in   time.Duration
		want string
	}{
		{-time.Second, "0 s"},
		{0, "0 s"},
		{400 * time.Millisecond, "0 s"},
		{45 * time.Second, "45 s"},
		{59*time.Second + 600*time.Millisecond, "1 min"},
		{time.Minute, "1 min"},
		{150 * time.Second, "2.5 min"},
		{8*time.Minute + 22*time.Second, "8.4 min"},
		{59*time.Minute + 58*time.Second, "1 h"},
		{90 * time.Minute, "1.5 h"},
		{26 * time.Hour, "26 h"},
	}
	for _, c := range cases {
		if got := Duration(c.in); got != c.want {
			t.Errorf("Duration(%v) = %q, want %q", c.in, got, c.want)
		}
	}
}
