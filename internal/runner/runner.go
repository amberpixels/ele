// Package runner spawns the real pg_restore and owns its process lifecycle. It
// forces --verbose and LC_ALL=C so the stderr stream is parseable, streams that
// stream line by line to a handler while teeing every raw line to a log, passes
// child stdout through untouched, and forwards SIGINT/SIGTERM to the child.
package runner

import (
	"bufio"
	"context"
	"errors"
	"io"
	"os"
	"os/exec"
	"os/signal"
	"syscall"
)

// Options configures a run. Command and Args are the process to launch; the
// rest wire up output. Zero values are sensible: Command defaults to
// "pg_restore", Stdout to os.Stdout, Stdin to os.Stdin.
type Options struct {
	Command string    // binary to run; default "pg_restore"
	Args    []string  // user's pg_restore argv (--verbose is added if missing)
	Stdout  io.Writer // child stdout passthrough; default os.Stdout
	Stdin   io.Reader // child stdin; default os.Stdin

	RawLog io.Writer    // if set, every raw stderr line is written here
	OnLine func(string) // if set, called with every raw stderr line
}

// Result reports how the child exited.
type Result struct {
	ExitCode int  // process exit code, or 128+signal if signaled
	Signaled bool // true if the child was killed by a signal
}

// Run launches the command, streams its stderr, and waits for it to exit.
// It returns an error only for failures to start or read; a nonzero child exit
// is reported in Result, not as an error.
func Run(ctx context.Context, opt Options) (*Result, error) {
	command := opt.Command
	if command == "" {
		command = "pg_restore"
	}

	cmd := exec.CommandContext(ctx, command, ensureVerbose(opt.Args)...)
	cmd.Env = append(os.Environ(), "LC_ALL=C") // deterministic message wording
	cmd.Stdin = orStdin(opt.Stdin)
	cmd.Stdout = orStdout(opt.Stdout)

	stderr, err := cmd.StderrPipe()
	if err != nil {
		return nil, err
	}

	if err := cmd.Start(); err != nil {
		return nil, err
	}

	stop := forwardSignals(cmd)
	defer stop()

	// Drain stderr in this goroutine: tee raw to the log and hand each line to
	// the parser. Done before Wait so the pipe is fully consumed.
	sc := bufio.NewScanner(stderr)
	sc.Buffer(make([]byte, 0, 64*1024), 4*1024*1024)
	for sc.Scan() {
		line := sc.Text()
		if opt.RawLog != nil {
			_, _ = io.WriteString(opt.RawLog, line+"\n")
		}
		if opt.OnLine != nil {
			opt.OnLine(line)
		}
	}
	scanErr := sc.Err()

	waitErr := cmd.Wait()
	if scanErr != nil {
		return nil, scanErr
	}
	return result(waitErr), nil
}

// ensureVerbose appends --verbose unless the user already asked for it, so the
// stream carries the per-item messages the parser needs.
func ensureVerbose(args []string) []string {
	for _, a := range args {
		if a == "-v" || a == "--verbose" {
			return args
		}
	}
	out := make([]string, len(args), len(args)+1)
	copy(out, args)
	return append(out, "--verbose")
}

// forwardSignals relays SIGINT/SIGTERM to the child and returns a stop func that
// tears the relay down.
func forwardSignals(cmd *exec.Cmd) func() {
	ch := make(chan os.Signal, 1)
	signal.Notify(ch, syscall.SIGINT, syscall.SIGTERM)
	done := make(chan struct{})
	go func() {
		for {
			select {
			case s := <-ch:
				if cmd.Process != nil {
					_ = cmd.Process.Signal(s)
				}
			case <-done:
				return
			}
		}
	}()
	return func() {
		signal.Stop(ch)
		close(done)
	}
}

// result translates the error from cmd.Wait into a Result. A signal death is
// reported as exit code 128+signal, the conventional shell encoding.
func result(waitErr error) *Result {
	if waitErr == nil {
		return &Result{ExitCode: 0}
	}
	if ee, ok := errors.AsType[*exec.ExitError](waitErr); ok {
		if ws, ok := ee.Sys().(syscall.WaitStatus); ok {
			if ws.Signaled() {
				return &Result{ExitCode: 128 + int(ws.Signal()), Signaled: true}
			}
			return &Result{ExitCode: ws.ExitStatus()}
		}
		return &Result{ExitCode: ee.ExitCode()}
	}
	return &Result{ExitCode: 1}
}

func orStdout(w io.Writer) io.Writer {
	if w != nil {
		return w
	}
	return os.Stdout
}

func orStdin(r io.Reader) io.Reader {
	if r != nil {
		return r
	}
	return os.Stdin
}
