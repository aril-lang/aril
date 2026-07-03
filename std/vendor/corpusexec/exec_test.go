package corpusexec

import (
	"strings"
	"testing"
	"time"
)

// cappedBuffer must retain at most cap bytes and report every write fully
// consumed, so a flooding writer can never grow it past the cap.
func TestCappedBufferCaps(t *testing.T) {
	b := cappedBuffer{cap: 16}
	n, err := b.Write([]byte(strings.Repeat("a", 100)))
	if err != nil || n != 100 {
		t.Fatalf("Write reported (%d, %v), want (100, nil)", n, err)
	}
	if got := b.String(); len(got) != 16 || got != strings.Repeat("a", 16) {
		t.Fatalf("retained %q (len %d), want 16 a's", got, len(got))
	}
	// A further write past the cap is still reported consumed but retains nothing.
	if n, _ := b.Write([]byte("bbbb")); n != 4 {
		t.Fatalf("second Write reported %d, want 4", n)
	}
	if len(b.String()) != 16 {
		t.Fatalf("buffer grew past cap to %d", len(b.String()))
	}
}

// A non-terminating example that floods stdout must be (a) killed by the
// wall-clock deadline (TimeoutCode) rather than hanging, and (b) capped in
// memory rather than accumulating without bound. This is the corpus-harness
// memory blow-up guarded against — see maxCaptureBytes.
func TestRunExampleCapsAndKillsFlood(t *testing.T) {
	done := make(chan *Result, 1)
	go func() {
		done <- RunExample("sh", []string{"-c", "while :; do printf 'xxxxxxxxxxxxxxxx'; done"}, "", "", 300)
	}()

	select {
	case r := <-done:
		if r.Code() != TimeoutCode {
			t.Fatalf("exit code %d, want TimeoutCode %d", r.Code(), TimeoutCode)
		}
		if len(r.Stdout()) > maxCaptureBytes {
			t.Fatalf("captured %d bytes, exceeds cap %d", len(r.Stdout()), maxCaptureBytes)
		}
	case <-time.After(10 * time.Second):
		t.Fatal("RunExample did not return — deadline/group-kill failed to bound the flood")
	}
}
