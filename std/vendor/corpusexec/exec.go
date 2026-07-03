// Package corpusexec is the OS-boundary adapter for the Aril corpus-status
// tool (tools/corpus-status). It is a vendored, self-contained Go module
// bound through the FFI (lang-spec/ffi.md §"Dependency model"), like
// std/vendor/arilkv.
//
// It exists because Aril's `(T, error) → Result<T, error>` boundary lift is
// *lossy*: it discards the value when the error is non-nil. A subprocess's
// combined output is meaningful precisely when the process *fails* (a build
// diagnostic), so `(*exec.Cmd).CombinedOutput()` cannot be bound directly —
// the diagnostic text would be thrown away on the non-zero exit the tool is
// trying to classify. This adapter reshapes the boundary into an opaque
// handle carrying both halves (output + exit code), each reachable through a
// value-returning method that the FFI binds cleanly.
package corpusexec

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"os/exec"
	"strings"
	"syscall"
	"time"
)

// TimeoutCode is the synthetic exit code RunExample reports when the process
// exceeds its deadline (mirrors coreutils `timeout`). It is non-zero, so the
// caller's "any non-zero is failure" rule already rejects a hung example.
const TimeoutCode = 124

// maxCaptureBytes bounds how much stdout/stderr RunExample retains from one
// run. A non-terminating example that floods its output would otherwise grow
// the capture buffer without limit — the harness process accumulates
// gigabytes before the wall-clock deadline fires, and that giant string then
// flows back out through the tool boundary (a corpus-harness memory blow-up,
// 2026-07). 8 MiB dwarfs any real corpus output (all hand-traced, well under a
// KiB) yet caps the bomb; a run truncated here already fails its output diff.
const maxCaptureBytes = 8 << 20

// cappedBuffer is an io.Writer retaining at most cap bytes and silently
// discarding the rest, so a runaway writer cannot exhaust memory. It keeps
// reporting writes fully consumed (never a short write) so the child is not
// perturbed by back-pressure; the wall-clock deadline still bounds its life.
type cappedBuffer struct {
	buf   bytes.Buffer
	limit int
}

func (c *cappedBuffer) Write(p []byte) (int, error) {
	if room := c.limit - c.buf.Len(); room > 0 {
		if len(p) > room {
			c.buf.Write(p[:room])
		} else {
			c.buf.Write(p)
		}
	}
	return len(p), nil
}

func (c *cappedBuffer) String() string { return c.buf.String() }

// Result is the opaque handle a run carries: the subprocess's combined
// stdout+stderr (for diagnostics), its stdout alone (for the run_ok output
// diff), and its exit code (0 on success).
type Result struct {
	out    string
	stdout string
	code   int
}

// Run executes name with args, capturing combined stdout+stderr. The exit
// code is 0 on success, the process's code on a normal non-zero exit, and
// -1 when the process could not be started (binary missing, etc.) — the
// caller treats any non-zero as failure, and the output carries the reason.
func Run(name string, args []string) *Result {
	c := exec.Command(name, args...)
	b, err := c.CombinedOutput()
	code := 0
	if err != nil {
		if ee, ok := err.(*exec.ExitError); ok {
			code = ee.ExitCode()
		} else {
			code = -1
		}
	}
	return &Result{out: string(b), code: code}
}

// RunExample executes a built example binary for the run_ok metric: it runs
// in working directory dir (so an example's argv/stdin file paths resolve
// relative to its own directory), feeds stdin, enforces a wall-clock timeout
// (timeoutMs; <= 0 means none), and captures stdout and stderr separately so
// the caller can diff stdout while still surfacing stderr in the combined
// output. The exit code is the process's on a normal exit, TimeoutCode on a
// deadline kill, and -1 when the process could not be started.
func RunExample(name string, args []string, stdin string, dir string, timeoutMs int) *Result {
	ctx := context.Background()
	cancel := context.CancelFunc(func() {})
	if timeoutMs > 0 {
		ctx, cancel = context.WithTimeout(ctx, time.Duration(timeoutMs)*time.Millisecond)
	}
	defer cancel()

	c := exec.CommandContext(ctx, name, args...)
	c.Dir = dir
	// Run the example in its own process group and, on cancel/timeout, kill the
	// whole group — not just the direct child — so a runaway example that
	// spawned helpers dies completely rather than leaking spinning orphans.
	// WaitDelay bounds Wait() itself in case a child holds the output pipe open.
	c.SysProcAttr = &syscall.SysProcAttr{Setpgid: true}
	c.Cancel = func() error {
		if c.Process == nil {
			return nil
		}
		return syscall.Kill(-c.Process.Pid, syscall.SIGKILL)
	}
	c.WaitDelay = 2 * time.Second
	if stdin != "" {
		c.Stdin = strings.NewReader(stdin)
	}
	outBuf := cappedBuffer{limit: maxCaptureBytes}
	errBuf := cappedBuffer{limit: maxCaptureBytes}
	c.Stdout = &outBuf
	c.Stderr = &errBuf
	err := c.Run()

	code := 0
	if err != nil {
		if ctx.Err() == context.DeadlineExceeded {
			code = TimeoutCode
		} else if ee, ok := err.(*exec.ExitError); ok {
			code = ee.ExitCode()
		} else {
			code = -1
		}
	}
	// out concatenates stdout then stderr (not the real-time interleave Run
	// gets from CombinedOutput); it is the diagnostic surface, while stdout is
	// the run_ok diff surface.
	return &Result{out: outBuf.String() + errBuf.String(), stdout: outBuf.String(), code: code}
}

// Sha256Hex returns the hex-encoded SHA-256 of s — the run-cache key over an
// example's emitted Go (plus its invocation). Aril has no crypto binding yet;
// this is the one-line adapter for it.
func Sha256Hex(s string) string {
	sum := sha256.Sum256([]byte(s))
	return hex.EncodeToString(sum[:])
}

// Bytes converts a string to a byte slice. Aril has no `[]byte(s)`
// conversion surface yet, and `os.WriteFile` needs `[]byte`; this is the
// one-line adapter for it.
func Bytes(s string) []byte { return []byte(s) }

// Out returns the captured combined output (valid on success and failure).
func (r *Result) Out() string { return r.out }

// Stdout returns the captured stdout alone (the run_ok output-diff surface).
func (r *Result) Stdout() string { return r.stdout }

// Code returns the exit code (0 = success).
func (r *Result) Code() int { return r.code }
