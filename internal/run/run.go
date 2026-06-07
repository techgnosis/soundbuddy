// Package run is a small, safe wrapper around external commands.
//
// Everything soundbuddy does is read-only, so this runner never needs stdin and
// always applies a short timeout. A missing binary is reported distinctly from a
// command that ran but failed, because the caller wants to say "tool not
// installed" rather than "tool errored".
package run

import (
	"context"
	"errors"
	"os/exec"
	"time"
)

// ErrNotFound means the executable was not present on PATH.
var ErrNotFound = errors.New("command not found")

// Result holds the outcome of running a command.
type Result struct {
	Stdout string
	Err    error // nil on success; ErrNotFound if the binary is missing
}

// OK reports whether the command produced usable output.
func (r Result) OK() bool { return r.Err == nil }

// Missing reports whether the command failed because the binary is absent.
func (r Result) Missing() bool { return errors.Is(r.Err, ErrNotFound) }

// Command runs name with args, capturing stdout. It applies a 5s timeout so a
// wedged helper can never hang the whole report.
func Command(name string, args ...string) Result {
	if _, err := exec.LookPath(name); err != nil {
		return Result{Err: ErrNotFound}
	}
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	out, err := exec.CommandContext(ctx, name, args...).Output()
	return Result{Stdout: string(out), Err: err}
}
