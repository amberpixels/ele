package runner

import (
	"bytes"
	"context"
	"reflect"
	"testing"
	"time"
)

func TestEnsureVerbose(t *testing.T) {
	tests := []struct {
		in, want []string
	}{
		{[]string{"-d", "db", "x.dump"}, []string{"-d", "db", "x.dump", "--verbose"}},
		{[]string{"--verbose", "-d", "db"}, []string{"--verbose", "-d", "db"}},
		{[]string{"-v", "-d", "db"}, []string{"-v", "-d", "db"}},
		{nil, []string{"--verbose"}},
	}
	for _, tt := range tests {
		if got := ensureVerbose(tt.in); !reflect.DeepEqual(got, tt.want) {
			t.Errorf("ensureVerbose(%v) = %v, want %v", tt.in, got, tt.want)
		}
	}
}

// sh runs a shell script as the "command", so the runner can be exercised
// without pg_restore. The --verbose the runner appends lands as $0 and is
// ignored by the script.
func shOpts(script string, o *Options) Options {
	opt := Options{Command: "/bin/sh", Args: []string{"-c", script}, Stdin: bytes.NewReader(nil)}
	if o != nil {
		opt.Stdout, opt.RawLog, opt.OnLine = o.Stdout, o.RawLog, o.OnLine
	}
	return opt
}

func TestRunStreamsAndTees(t *testing.T) {
	var stdout, rawlog bytes.Buffer
	var lines []string

	opt := shOpts(`printf 'a\nb\nc\n' >&2; printf 'stdout-line\n'; exit 0`, &Options{
		Stdout: &stdout,
		RawLog: &rawlog,
		OnLine: func(s string) { lines = append(lines, s) },
	})

	res, err := Run(context.Background(), opt)
	if err != nil {
		t.Fatalf("Run: %v", err)
	}
	if res.ExitCode != 0 || res.Signaled {
		t.Errorf("result = %+v, want exit 0", res)
	}
	if want := []string{"a", "b", "c"}; !reflect.DeepEqual(lines, want) {
		t.Errorf("OnLine lines = %v, want %v", lines, want)
	}
	if rawlog.String() != "a\nb\nc\n" {
		t.Errorf("raw log = %q", rawlog.String())
	}
	if stdout.String() != "stdout-line\n" {
		t.Errorf("stdout passthrough = %q", stdout.String())
	}
}

func TestRunPropagatesExitCode(t *testing.T) {
	res, err := Run(context.Background(), shOpts(`exit 3`, nil))
	if err != nil {
		t.Fatalf("Run: %v", err)
	}
	if res.ExitCode != 3 || res.Signaled {
		t.Errorf("result = %+v, want exit 3, not signaled", res)
	}
}

func TestRunSignaledOnContextCancel(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 50*time.Millisecond)
	defer cancel()

	res, err := Run(ctx, shOpts(`sleep 5`, nil))
	if err != nil {
		t.Fatalf("Run: %v", err)
	}
	if !res.Signaled {
		t.Errorf("result = %+v, want signaled after cancel", res)
	}
}
