package main

import (
	"strings"
	"testing"
)

// Coverage for the version-constraint grammar, semver ordering, and the MVS
// engine (RFC-0008 §Version identifiers / §Version constraints / §Resolution).

func mustSemver(t *testing.T, s string) semver {
	t.Helper()
	v, err := parseSemver(s)
	if err != nil {
		t.Fatalf("parseSemver(%q): %v", s, err)
	}
	return v
}

func TestSemverOrdering(t *testing.T) {
	cases := []struct {
		a, b string
		want int
	}{
		{"v1.2.3", "v1.2.3", 0},
		{"v1.2.3", "v1.2.4", -1},
		{"v1.3.0", "v1.2.9", 1},
		{"v2.0.0", "v1.9.9", 1},
		{"v1.0.0-alpha", "v1.0.0", -1},      // a pre-release is below its release
		{"v1.0.0-alpha", "v1.0.0-beta", -1}, // alphanumeric lexical
		{"v1.0.0-alpha.1", "v1.0.0-alpha.2", -1},
		{"v1.0.0-alpha.1", "v1.0.0-alpha", 1}, // more identifiers > fewer
		{"v1.0.0-1", "v1.0.0-alpha", -1},      // numeric < alphanumeric
		{"v1.0.0+build", "v1.0.0", 0},         // build metadata ignored
	}
	for _, c := range cases {
		got := mustSemver(t, c.a).compare(mustSemver(t, c.b))
		if got != c.want {
			t.Errorf("compare(%q,%q) = %d; want %d", c.a, c.b, got, c.want)
		}
	}
}

func TestParseSemverRequiresV(t *testing.T) {
	if _, err := parseSemver("1.2.3"); err == nil {
		t.Error("a bare 1.2.3 (no v) must be rejected by parseSemver")
	}
}

func TestConstraintRanges(t *testing.T) {
	// (constraint, version, admitted?) — the RFC-0008 §Version constraints table.
	cases := []struct {
		cons, ver string
		admit     bool
	}{
		{"^1.3", "v1.3.0", true},
		{"^1.3", "v1.9.9", true},
		{"^1.3", "v2.0.0", false},
		{"^1.3", "v1.2.9", false},
		{"^0.4.2", "v0.4.2", true},
		{"^0.4.2", "v0.4.9", true},
		{"^0.4.2", "v0.5.0", false}, // 0.x: minor is the breaking axis
		{"^0.4.2", "v0.4.1", false},
		{"^0.0.5", "v0.0.5", true},
		{"^0.0.5", "v0.0.6", false}, // effectively exact
		{"~1.3.2", "v1.3.2", true},
		{"~1.3.2", "v1.3.9", true},
		{"~1.3.2", "v1.4.0", false}, // tilde is patch-only
		{"1.3.*", "v1.3.0", true},
		{"1.3.*", "v1.3.7", true},
		{"1.3.*", "v1.4.0", false},
		{">=1.3, <1.6", "v1.5.9", true},
		{">=1.3, <1.6", "v1.6.0", false},
		{">=1.3, <1.6", "v1.2.0", false},
		{"=v1.3.0", "v1.3.0", true},
		{"=v1.3.0", "v1.3.1", false},
		{"v1.3.0", "v1.3.0", true}, // bare tag = exact pin
		{"v1.3.0", "v1.3.1", false},
	}
	for _, c := range cases {
		cons, err := parseConstraint(c.cons)
		if err != nil {
			t.Errorf("parseConstraint(%q): %v", c.cons, err)
			continue
		}
		if got := cons.admits(mustSemver(t, c.ver)); got != c.admit {
			t.Errorf("%q admits %q = %v; want %v", c.cons, c.ver, got, c.admit)
		}
	}
}

func TestParseConstraintBareTagDiagnostic(t *testing.T) {
	// A bare `1.2.3` (no v, no operator) is a targeted diagnostic nudging to v1.2.3.
	_, err := parseConstraint("1.2.3")
	if err == nil {
		t.Fatal("a bare 1.2.3 should be a diagnostic")
	}
	if !strings.Contains(err.Error(), "v1.2.3") {
		t.Errorf("the diagnostic should suggest the `v` spelling, got: %v", err)
	}
}

func TestParseConstraintCommitSHA(t *testing.T) {
	sha := "0123456789abcdef0123456789abcdef01234567"
	c, err := parseConstraint(sha)
	if err != nil {
		t.Fatalf("SHA constraint: %v", err)
	}
	pin, ok := c.exactPin()
	if !ok || pin != sha {
		t.Errorf("exactPin(SHA) = %q,%v; want the SHA", pin, ok)
	}
}

func TestConstraintExactPin(t *testing.T) {
	exact := []string{"v1.2.0", "=v1.2.0"}
	for _, s := range exact {
		c, _ := parseConstraint(s)
		if pin, ok := c.exactPin(); !ok || pin != "v1.2.0" {
			t.Errorf("exactPin(%q) = %q,%v; want v1.2.0,true", s, pin, ok)
		}
	}
	ranged := []string{"^1.3", "~1.3.2", "1.3.*", ">=1.3, <1.6"}
	for _, s := range ranged {
		c, _ := parseConstraint(s)
		if _, ok := c.exactPin(); ok {
			t.Errorf("exactPin(%q) should be false (ranged)", s)
		}
	}
}

func TestMVSMaxOfFloors(t *testing.T) {
	// Two requirers of one module: the max of the floors is selected (MVS).
	a, _ := parseConstraint("^1.2") // floor 1.2.0
	b, _ := parseConstraint("^1.4") // floor 1.4.0
	sel, err := mvsSelect("kv", []requirement{{"A", a}, {"B", b}}, nil)
	if err != nil {
		t.Fatalf("mvsSelect: %v", err)
	}
	if sel.compare(mustSemver(t, "v1.4.0")) != 0 {
		t.Errorf("selected = %s; want v1.4.0 (max of floors)", sel)
	}
}

func TestMVSUpperBoundConflict(t *testing.T) {
	// A requires <2.0 but another forces the floor to 2.x → fail closed, naming
	// both requirers and pointing at `aril upgrade`.
	a, _ := parseConstraint(">=1.0, <2.0")
	b, _ := parseConstraint("^2.1") // floor 2.1.0, violates a's <2.0
	_, err := mvsSelect("kv", []requirement{{"A", a}, {"B", b}}, nil)
	if err == nil {
		t.Fatal("expected an upper-bound gate conflict")
	}
	for _, want := range []string{"E0122", "aril upgrade", "A"} {
		if !strings.Contains(err.Error(), want) {
			t.Errorf("conflict message missing %q: %v", want, err)
		}
	}
}

func TestMVSSnapsToCandidateTag(t *testing.T) {
	// With a candidate tag list, MVS snaps the floor up to the lowest real tag ≥ floor.
	c, _ := parseConstraint("^1.2") // floor 1.2.0
	candidates := []semver{
		mustSemver(t, "v1.1.0"),
		mustSemver(t, "v1.2.5"), // lowest ≥ 1.2.0
		mustSemver(t, "v1.9.0"),
	}
	sel, err := mvsSelect("kv", []requirement{{"root", c}}, candidates)
	if err != nil {
		t.Fatalf("mvsSelect: %v", err)
	}
	if sel.compare(mustSemver(t, "v1.2.5")) != 0 {
		t.Errorf("selected = %s; want v1.2.5 (lowest tag ≥ floor)", sel)
	}
}

func TestMVSExclusiveFloorSkipsExcludedTag(t *testing.T) {
	// An exclusive `>1.2.0` floor must NOT select the excluded v1.2.0 itself:
	// MVS picks the lowest candidate that *satisfies* the constraint (v1.3.0),
	// not merely the lowest tag ≥ the floor value (regression: PR#142 review).
	c, _ := parseConstraint(">1.2.0, <2.0.0")
	candidates := []semver{mustSemver(t, "v1.2.0"), mustSemver(t, "v1.3.0")}
	sel, err := mvsSelect("m", []requirement{{"root", c}}, candidates)
	if err != nil {
		t.Fatalf("a satisfiable `>` range must resolve, got: %v", err)
	}
	if sel.compare(mustSemver(t, "v1.3.0")) != 0 {
		t.Errorf("selected = %s; want v1.3.0 (v1.2.0 is excluded by `>1.2.0`)", sel)
	}
}
