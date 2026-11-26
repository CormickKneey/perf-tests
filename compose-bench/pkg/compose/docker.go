package compose

import (
	"bytes"
	"context"
	"fmt"
	"io"
	"os"
	"os/exec"
	"strings"
	"time"
)

// Exec runs host commands (docker, docker compose, crane) and logs them.
type Exec struct {
	Log io.Writer
	// Env is appended to the filtered process environment.
	Env []string
}

func (e *Exec) logf(format string, a ...any) {
	if e.Log != nil {
		fmt.Fprintf(e.Log, time.Now().Format("15:04:05")+" "+format+"\n", a...)
	}
}

func (e *Exec) environ(extra []string) []string {
	var env []string
	for _, kv := range os.Environ() {
		// Never let a stray shell DFPERF_* variable override the rendered
		// project environment (compose gives the shell precedence).
		if strings.HasPrefix(kv, "DFPERF_") || strings.HasPrefix(kv, "COMPOSE_PROJECT_NAME=") || strings.HasPrefix(kv, "COMPOSE_PROFILES=") || strings.HasPrefix(kv, "COMPOSE_FILE=") {
			continue
		}
		env = append(env, kv)
	}
	env = append(env, e.Env...)
	return append(env, extra...)
}

// Output runs a command and returns trimmed stdout. stderr is included in
// the error.
func (e *Exec) Output(ctx context.Context, name string, args ...string) (string, error) {
	return e.OutputEnv(ctx, nil, name, args...)
}

// OutputEnv is Output with extra environment variables.
func (e *Exec) OutputEnv(ctx context.Context, extra []string, name string, args ...string) (string, error) {
	cmd := exec.CommandContext(ctx, name, args...)
	cmd.Env = e.environ(extra)
	var out, errb bytes.Buffer
	cmd.Stdout, cmd.Stderr = &out, &errb
	err := cmd.Run()
	if err != nil {
		return strings.TrimSpace(out.String()), fmt.Errorf("%s %s: %w: %s", name, strings.Join(args, " "), err, strings.TrimSpace(tailStr(errb.String(), 2000)))
	}
	return strings.TrimSpace(out.String()), nil
}

// Stream runs a command with stdout/stderr going to w (and the log) and
// returns the exit code.
func (e *Exec) Stream(ctx context.Context, w io.Writer, extra []string, name string, args ...string) (int, error) {
	e.logf("$ %s %s", name, strings.Join(args, " "))
	cmd := exec.CommandContext(ctx, name, args...)
	cmd.Env = e.environ(extra)
	if w == nil {
		w = io.Discard
	}
	cmd.Stdout, cmd.Stderr = w, w
	err := cmd.Run()
	if err != nil {
		if ee, ok := err.(*exec.ExitError); ok {
			return ee.ExitCode(), nil
		}
		return -1, err
	}
	return 0, nil
}

func tailStr(s string, n int) string {
	if len(s) <= n {
		return s
	}
	return s[len(s)-n:]
}
