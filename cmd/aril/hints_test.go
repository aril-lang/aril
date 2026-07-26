package main

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/aril-lang/aril/internal/sema"
)

// discardsResultProgram builds and runs, but drops a Result — so it must emit
// H0001 on stderr yet still exit 0 (the point of the non-blocking tier, D58).
const discardsResultProgram = `import fmt

func risky(): Result<int, error> {
	return Ok(1)
}

func main() {
	risky()
	fmt.println("done")
}
`

// TestHintRendersButBuildStaysGreen is the end-to-end guard: a discarded
// Result emits the H0001 hint on stderr, and the program still runs to a
// clean exit — a hint never breaks the build (D58 / AUDIT-3 T16).
func TestHintRendersButBuildStaysGreen(t *testing.T) {
	src := filepath.Join(t.TempDir(), "discard.aril")
	if err := os.WriteFile(src, []byte(discardsResultProgram), 0o644); err != nil {
		t.Fatal(err)
	}

	stdout, stderr, exit := runAril(t, "run", src)
	if exit != 0 {
		t.Fatalf("run exited %d (a hint must not fail the build); stderr:\n%s", exit, stderr)
	}
	if strings.TrimSpace(stdout) != "done" {
		t.Errorf("stdout = %q; want \"done\"", stdout)
	}
	if !strings.Contains(stderr, "hint[H0001]") {
		t.Errorf("expected H0001 hint on stderr, got:\n%s", stderr)
	}

	// -hints=off suppresses the hint; the run still succeeds.
	stdout, stderr, exit = runAril(t, "run", "-hints=off", src)
	if exit != 0 {
		t.Fatalf("run -hints=off exited %d; stderr:\n%s", exit, stderr)
	}
	if strings.Contains(stderr, "H0001") {
		t.Errorf("-hints=off should suppress H0001, got:\n%s", stderr)
	}
	if strings.TrimSpace(stdout) != "done" {
		t.Errorf("stdout = %q; want \"done\"", stdout)
	}
}

// TestResolveHintPolicy: the flag overrides the env, the env grammar
// (off / off:codes / on) parses, and an unknown value fails open (hints on).
func TestResolveHintPolicy(t *testing.T) {
	tests := []struct {
		name       string
		flag       string
		env        string
		envSet     bool
		showH0001  bool
		showH0002  bool
		wantAllOff bool
	}{
		{name: "default on", showH0001: true, showH0002: true},
		{name: "flag off", flag: "off", wantAllOff: true},
		{name: "flag on beats env off", flag: "on", env: "off", envSet: true, showH0001: true, showH0002: true},
		{name: "env off", env: "off", envSet: true, wantAllOff: true},
		{name: "env on", env: "on", envSet: true, showH0001: true, showH0002: true},
		{name: "env off:H0001 suppresses one", env: "off:H0001", envSet: true, showH0001: false, showH0002: true},
		{name: "env off:H0001,H0002 suppresses list", env: "off:H0001, H0002", envSet: true, showH0001: false, showH0002: false},
		{name: "env garbage fails open", env: "wat", envSet: true, showH0001: true, showH0002: true},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			if tc.envSet {
				t.Setenv("ARIL_HINTS", tc.env)
			} else {
				t.Setenv("ARIL_HINTS", "") // t.Setenv restores after the test
			}
			p := resolveHintPolicy(tc.flag)
			if p.allOff != tc.wantAllOff {
				t.Errorf("allOff = %v, want %v", p.allOff, tc.wantAllOff)
			}
			if got := p.show("H0001"); got != tc.showH0001 {
				t.Errorf("show(H0001) = %v, want %v", got, tc.showH0001)
			}
			if got := p.show("H0002"); got != tc.showH0002 {
				t.Errorf("show(H0002) = %v, want %v", got, tc.showH0002)
			}
		})
	}
}

// TestPrintDiagsBlocking: printDiags reports blocking only for error-severity
// diagnostics; a hint alone never blocks (D58).
func TestPrintDiagsBlocking(t *testing.T) {
	hintOnly := []*sema.Diag{{Code: "H0001", Message: "x", Severity: sema.SeverityHint}}
	if printDiags(hintOnly, hintPolicy{}) {
		t.Error("a lone hint must not be blocking")
	}
	// Suppressed hints also do not block.
	if printDiags(hintOnly, hintPolicy{allOff: true}) {
		t.Error("a suppressed hint must not be blocking")
	}
	withError := []*sema.Diag{
		{Code: "H0001", Message: "x", Severity: sema.SeverityHint},
		{Code: "E0201", Message: "y"},
	}
	if !printDiags(withError, hintPolicy{}) {
		t.Error("an error-severity diagnostic must be blocking")
	}
}
