// Package runner runs a program with keys in its environment: only the
// keys asked for, only in the child, never on disk.
package runner

import (
	"bytes"
	"context"
	"errors"
	"io"
	"os"
	"os/exec"
	"runtime"
	"strings"
	"time"

	"keybar/redact"
)

// Env is base with inject's variables set, replacing what base had for
// them (case-insensitively on Windows, as its environment is).
func Env(base []string, inject map[string][]byte) []string {
	same := func(a, b string) bool {
		if runtime.GOOS == "windows" {
			return strings.EqualFold(a, b)
		}
		return a == b
	}
	out := make([]string, 0, len(base)+len(inject))
	for _, kv := range base {
		name, _, _ := strings.Cut(kv, "=")
		drop := false
		for v := range inject {
			if same(name, v) {
				drop = true
				break
			}
		}
		if !drop {
			out = append(out, kv)
		}
	}
	for v, val := range inject {
		out = append(out, v+"="+string(val))
	}
	return out
}

// Command is argv with the variables, in dir.
func Command(ctx context.Context, argv []string, inject map[string][]byte, dir string) (*exec.Cmd, error) {
	if len(argv) == 0 || argv[0] == "" {
		return nil, errors.New("没有要运行的命令")
	}
	cmd := exec.CommandContext(ctx, argv[0], argv[1:]...)
	cmd.Env = Env(os.Environ(), inject)
	cmd.Dir = dir
	cmd.WaitDelay = 2 * time.Second
	return cmd, nil
}

// Shell is a command line run by the system's shell, for a command given
// as one string.
func Shell(line string) []string {
	if runtime.GOOS == "windows" {
		return []string{"cmd", "/C", line}
	}
	return []string{"/bin/sh", "-c", line}
}

// Result is a captured run.
type Result struct {
	Exit      int
	Stdout    string
	Stderr    string
	Truncated bool
	TimedOut  bool
	Err       error // the program could not start
}

// MaxOutput is the most of each stream kept.
const MaxOutput = 256 << 10

// slack is more than the longest value's longest encoding.
const slack = 16 << 10

func (c *capped) text(r *redact.Redactor) string {
	s := r.String(c.b.String())
	if c.over && len(s) > MaxOutput {
		s = s[:MaxOutput] + "\n…（输出过长，已截断）"
	}
	return s
}

type capped struct {
	b    bytes.Buffer
	over bool
}

func (c *capped) Write(p []byte) (int, error) {
	// Kept a little past MaxOutput, so that no value is cut in two
	// before it is redacted; cut after.
	if left := MaxOutput + slack - c.b.Len(); left > 0 {
		c.b.Write(p[:min(len(p), left)])
	}
	if c.b.Len() > MaxOutput {
		c.over = true
	}
	return len(p), nil
}

// Capture runs argv with the variables in inject and returns its output,
// with the values in secrets (key name to value: the label shown in
// [REDACTED:NAME]) redacted; nil secrets redacts inject's values under
// the variables' names.
func Capture(ctx context.Context, argv []string, inject, secrets map[string][]byte, dir string, timeout time.Duration, stdin io.Reader) Result {
	ctx, cancel := context.WithTimeout(ctx, timeout)
	defer cancel()
	cmd, err := Command(ctx, argv, inject, dir)
	if err != nil {
		return Result{Exit: -1, Err: err}
	}
	if secrets == nil {
		secrets = inject
	}
	r := redact.New(secrets)
	var out, errb capped
	cmd.Stdout, cmd.Stderr, cmd.Stdin = &out, &errb, stdin
	err = cmd.Run()
	res := Result{Stdout: out.text(r), Stderr: errb.text(r), Truncated: out.over || errb.over}
	var ee *exec.ExitError
	switch {
	case ctx.Err() == context.DeadlineExceeded:
		res.TimedOut, res.Exit = true, -1
	case errors.As(err, &ee):
		res.Exit = ee.ExitCode()
	case err != nil:
		res.Exit, res.Err = -1, errors.New(r.String(err.Error()))
	}
	return res
}

// Display renders argv for a person to read and approve: each argument
// that is not plainly a word is single-quoted, shell style, so the
// boundaries between arguments are visible.
func Display(argv []string) string {
	parts := make([]string, len(argv))
	for i, a := range argv {
		plain := a != ""
		for _, r := range a {
			if !(r >= 'a' && r <= 'z' || r >= 'A' && r <= 'Z' || r >= '0' && r <= '9' || strings.ContainsRune("-_./=:,@+%", r)) {
				plain = false
				break
			}
		}
		if plain {
			parts[i] = a
		} else {
			parts[i] = "'" + strings.ReplaceAll(a, "'", `'\''`) + "'"
		}
	}
	return strings.Join(parts, " ")
}
