package main

// version.go — the version-constraint grammar, semver ordering, and the
// minimal-version-selection (MVS) engine (RFC-0008 §Version identifiers /
// §Version constraints / §Resolution). This is the pure, offline resolver
// core: it parses caret/tilde/wildcard/compound/exact constraints, orders
// semver releases, and selects the max-of-floors for a module while gating
// every declared upper bound. Fetching (git tag enumeration) rides on top of
// it (later in this epoch); nothing here touches the network or the disk.

import (
	"fmt"
	"strconv"
	"strings"
)

// semver is a parsed `vX.Y.Z[-pre]` release version (RFC-0008: the `v` prefix
// is required on the wire; `+meta` is accepted and ignored for ordering, per
// semver.org §10). Pre-release lowers precedence (§9/§11).
type semver struct {
	major, minor, patch int
	pre                 string // pre-release without the leading '-'; "" if none
}

func (v semver) String() string {
	s := fmt.Sprintf("v%d.%d.%d", v.major, v.minor, v.patch)
	if v.pre != "" {
		s += "-" + v.pre
	}
	return s
}

// parseSemver parses a full `vX.Y.Z[-pre][+meta]` version. The `v` prefix is
// required (RFC-0008 §Version identifiers); a bare `1.2.3` is a caller-level
// diagnostic, not parsed here.
func parseSemver(s string) (semver, error) {
	if !strings.HasPrefix(s, "v") {
		return semver{}, fmt.Errorf("a version tag is written `v%s`, not `%s` (the `v` prefix is required)", s, s)
	}
	return parseSemverCore(s[1:])
}

// parseSemverCore parses the `X.Y.Z[-pre][+meta]` body (no `v`), filling
// omitted minor/patch with 0 (`1.3` → 1.3.0) — the partial form the
// caret/tilde/wildcard constraints carry.
func parseSemverCore(body string) (semver, error) {
	// Strip build metadata (+meta): accepted, ignored for ordering.
	if i := strings.IndexByte(body, '+'); i >= 0 {
		body = body[:i]
	}
	var pre string
	if i := strings.IndexByte(body, '-'); i >= 0 {
		pre = body[i+1:]
		body = body[:i]
	}
	parts := strings.Split(body, ".")
	if len(parts) == 0 || len(parts) > 3 {
		return semver{}, fmt.Errorf("malformed version %q", body)
	}
	nums := [3]int{}
	for i := 0; i < 3; i++ {
		if i >= len(parts) {
			nums[i] = 0 // partial: omitted component defaults to 0
			continue
		}
		n, err := strconv.Atoi(parts[i])
		if err != nil || n < 0 {
			return semver{}, fmt.Errorf("malformed version component %q", parts[i])
		}
		nums[i] = n
	}
	return semver{major: nums[0], minor: nums[1], patch: nums[2], pre: pre}, nil
}

// compare orders two versions by semver precedence (§11): major, minor, patch
// numerically, then a pre-release version below its associated normal version.
// Returns -1, 0, or +1.
func (v semver) compare(o semver) int {
	if c := cmpInt(v.major, o.major); c != 0 {
		return c
	}
	if c := cmpInt(v.minor, o.minor); c != 0 {
		return c
	}
	if c := cmpInt(v.patch, o.patch); c != 0 {
		return c
	}
	// Equal core: a pre-release has LOWER precedence than no pre-release.
	if v.pre == o.pre {
		return 0
	}
	if v.pre == "" {
		return 1
	}
	if o.pre == "" {
		return -1
	}
	return comparePre(v.pre, o.pre)
}

// comparePre orders two pre-release strings per semver §11: dot-separated
// identifiers, numeric compared numerically (and below alphanumeric), a longer
// set of identifiers above a shorter when all preceding are equal.
func comparePre(a, b string) int {
	as, bs := strings.Split(a, "."), strings.Split(b, ".")
	for i := 0; i < len(as) && i < len(bs); i++ {
		x, y := as[i], bs[i]
		if x == y {
			continue
		}
		xn, xerr := strconv.Atoi(x)
		yn, yerr := strconv.Atoi(y)
		switch {
		case xerr == nil && yerr == nil:
			return cmpInt(xn, yn)
		case xerr == nil: // numeric < alphanumeric
			return -1
		case yerr == nil:
			return 1
		default:
			if x < y {
				return -1
			}
			return 1
		}
	}
	return cmpInt(len(as), len(bs))
}

func cmpInt(a, b int) int {
	switch {
	case a < b:
		return -1
	case a > b:
		return 1
	default:
		return 0
	}
}

// comparator is one relational term of a constraint (`>=1.3`, `<1.6`, `=v1.2.0`).
type comparator struct {
	op string // ">=" | ">" | "<=" | "<" | "="
	v  semver
}

func (cm comparator) admits(v semver) bool {
	c := v.compare(cm.v)
	switch cm.op {
	case ">=":
		return c >= 0
	case ">":
		return c > 0
	case "<=":
		return c <= 0
	case "<":
		return c < 0
	case "=":
		return c == 0
	}
	return false
}

// constraint is a parsed `version` requirement (RFC-0008 §Version constraints):
// a set of comparators the selected version must jointly satisfy. An exact
// commit SHA is carried verbatim (it has no semver ordering) via commit.
type constraint struct {
	text   string       // the original spelling, for diagnostics
	comps  []comparator // AND-ed relational terms (empty ⇒ wildcard `*`)
	commit string       // a 40-hex commit-SHA pin (mutually exclusive with comps)
}

// admits reports whether v satisfies every comparator (a SHA-pin admits no
// semver — it is matched at the fetch layer by identity).
func (c constraint) admits(v semver) bool {
	if c.commit != "" {
		return false
	}
	for _, cm := range c.comps {
		if !cm.admits(v) {
			return false
		}
	}
	return true
}

// floor returns the inclusive lower bound the constraint imposes for MVS
// (max-of-floors), and whether it has one. `>1.2.0` has no *inclusive* floor
// at 1.2.0 — it is treated as the exclusive bound it is via admits() during the
// gate; floor() reports 1.2.0 as the ordering key, sound because the eventual
// selected tag must still pass admits().
func (c constraint) floor() (semver, bool) {
	var lo semver
	has := false
	for _, cm := range c.comps {
		if cm.op == ">=" || cm.op == ">" || cm.op == "=" {
			if !has || cm.v.compare(lo) > 0 {
				lo, has = cm.v, true
			}
		}
	}
	return lo, has
}

// exactPin returns the single concrete version a constraint pins to when it is
// degenerate — an exact tag (`v1.2.0` / `=v1.2.0`) or a commit SHA — for the
// fetch layer. A ranged constraint returns ("", false).
func (c constraint) exactPin() (string, bool) {
	if c.commit != "" {
		return c.commit, true
	}
	if len(c.comps) == 1 && c.comps[0].op == "=" {
		return c.comps[0].v.String(), true
	}
	return "", false
}

// parseConstraint parses a `version` value into a constraint (RFC-0008
// §Version constraints). It accepts the caret/tilde/wildcard/compound/exact
// range spellings the TS audience reads fluently, an exact `vX.Y.Z` tag (the
// pre-revision spelling, still an exact pin), and a 40-hex commit SHA. A bare
// `1.2.3` (no `v`, no operator) is a targeted diagnostic.
func parseConstraint(s string) (constraint, error) {
	raw := strings.TrimSpace(s)
	if raw == "" {
		return constraint{}, fmt.Errorf("empty version constraint")
	}
	c := constraint{text: raw}
	switch {
	case isFullCommitSHA(raw):
		c.commit = raw
		return c, nil
	case raw == "*":
		return c, nil // wildcard: any version
	case strings.HasPrefix(raw, "^"):
		return caretConstraint(raw)
	case strings.HasPrefix(raw, "~"):
		return tildeConstraint(raw)
	case strings.HasSuffix(raw, ".*"):
		return wildcardConstraint(raw)
	case strings.ContainsAny(raw, "<>") || strings.Contains(raw, ","):
		return compoundConstraint(raw)
	case strings.HasPrefix(raw, "="):
		v, err := parseSemver(strings.TrimSpace(raw[1:]))
		if err != nil {
			return constraint{}, err
		}
		c.comps = []comparator{{"=", v}}
		return c, nil
	case strings.HasPrefix(raw, "v"):
		// A bare `vX.Y.Z` tag is the pre-revision exact-pin spelling.
		v, err := parseSemver(raw)
		if err != nil {
			return constraint{}, err
		}
		c.comps = []comparator{{"=", v}}
		return c, nil
	default:
		// A bare `1.2.3` — not a valid range and missing the required `v`.
		if _, err := parseSemverCore(raw); err == nil {
			return constraint{}, fmt.Errorf("a version tag is written `v%s`, not `%s` (the `v` prefix is required); for a range use a caret `^%s`", raw, raw, raw)
		}
		return constraint{}, fmt.Errorf("unrecognized version constraint %q (want ^x.y, ~x.y.z, x.y.*, >=x.y,<x.z, =vX.Y.Z, or a commit SHA)", raw)
	}
}

// caretConstraint: `^X.Y.Z` floats up to the next breaking axis — for 0.x the
// left-most non-zero component is the breaking axis (strict npm/Cargo caret).
func caretConstraint(raw string) (constraint, error) {
	lo, err := parseSemverCore(strings.TrimPrefix(raw, "^"))
	if err != nil {
		return constraint{}, fmt.Errorf("in %q: %w", raw, err)
	}
	var hi semver
	switch {
	case lo.major > 0:
		hi = semver{major: lo.major + 1}
	case lo.minor > 0:
		hi = semver{minor: lo.minor + 1}
	default:
		hi = semver{patch: lo.patch + 1}
	}
	return constraint{text: raw, comps: []comparator{{">=", lo}, {"<", hi}}}, nil
}

// tildeConstraint: `~X.Y.Z`/`~X.Y` allow patch-level drift; `~X` allows minor.
func tildeConstraint(raw string) (constraint, error) {
	body := strings.TrimPrefix(raw, "~")
	lo, err := parseSemverCore(body)
	if err != nil {
		return constraint{}, fmt.Errorf("in %q: %w", raw, err)
	}
	var hi semver
	if len(strings.Split(bodyCore(body), ".")) >= 2 {
		hi = semver{major: lo.major, minor: lo.minor + 1} // patch-only
	} else {
		hi = semver{major: lo.major + 1} // only-major given: minor allowed
	}
	return constraint{text: raw, comps: []comparator{{">=", lo}, {"<", hi}}}, nil
}

// wildcardConstraint: `X.Y.*` → [X.Y.0, X.(Y+1).0); `X.*` → [X.0.0, (X+1).0.0).
func wildcardConstraint(raw string) (constraint, error) {
	stem := strings.TrimSuffix(raw, ".*")
	lo, err := parseSemverCore(stem)
	if err != nil {
		return constraint{}, fmt.Errorf("in %q: %w", raw, err)
	}
	var hi semver
	if strings.Contains(stem, ".") { // X.Y.* → minor fixed, patch floats
		hi = semver{major: lo.major, minor: lo.minor + 1}
	} else { // X.* → major fixed, minor floats
		hi = semver{major: lo.major + 1}
	}
	return constraint{text: raw, comps: []comparator{{">=", lo}, {"<", hi}}}, nil
}

// compoundConstraint parses comma-separated relational terms (`>=1.3, <1.6`).
func compoundConstraint(raw string) (constraint, error) {
	c := constraint{text: raw}
	for _, part := range strings.Split(raw, ",") {
		part = strings.TrimSpace(part)
		if part == "" {
			continue
		}
		op := "="
		for _, cand := range []string{">=", "<=", ">", "<", "="} {
			if strings.HasPrefix(part, cand) {
				op = cand
				part = strings.TrimSpace(part[len(cand):])
				break
			}
		}
		v, err := parseFlexSemver(part)
		if err != nil {
			return constraint{}, fmt.Errorf("in %q: %w", raw, err)
		}
		c.comps = append(c.comps, comparator{op, v})
	}
	if len(c.comps) == 0 {
		return constraint{}, fmt.Errorf("empty compound constraint %q", raw)
	}
	return c, nil
}

// parseFlexSemver parses a comparator operand, tolerating an optional `v`
// prefix (`>=v1.3` and `>=1.3` both read).
func parseFlexSemver(s string) (semver, error) {
	return parseSemverCore(strings.TrimPrefix(s, "v"))
}

// bodyCore strips a pre-release/meta suffix so the component count reflects the
// numeric fields only (used by tilde to decide the drift axis).
func bodyCore(body string) string {
	if i := strings.IndexAny(body, "-+"); i >= 0 {
		return body[:i]
	}
	return body
}

// requirement pairs a constraint with the module that declared it, so an MVS
// conflict can name both requirers (RFC-0008 §Resolution / E0122).
type requirement struct {
	by string // the requiring module ("" for the root)
	c  constraint
}

// mvsSelect applies minimal version selection to one module's requirements.
// With a candidate tag list (git-tag enumeration passes it in), the selection
// is the **lowest released tag that satisfies every requirement** — which is
// exactly the max-of-floors under every declared ceiling, and correctly
// excludes an exclusive `>X` floor's own boundary version. Without candidates
// (the pure-core path), it returns the max-of-floors and gates it. A genuine
// conflict returns E0122, naming the requirers and pointing at the `aril
// upgrade` manual-backtracking substitute (the accepted MVS incompleteness,
// RFC-0008 §Resolution).
func mvsSelect(module string, reqs []requirement, candidates []semver) (semver, error) {
	if len(candidates) > 0 {
		if sel, ok := lowestSatisfying(candidates, reqs); ok {
			return sel, nil
		}
		return semver{}, mvsConflict(module, reqs)
	}
	// No candidate list: select the max-of-floors and gate it. This is sound
	// for inclusive floors; an exclusive-only `>X` floor cannot be snapped to a
	// real tag here (no tag list) and, failing its own gate, reports a conflict.
	floor, ok := maxFloor(reqs)
	if !ok {
		return semver{}, fmt.Errorf("aril: error[E0122]: dependency %q has no lower-bounded version requirement to select from", module)
	}
	for _, r := range reqs {
		if !r.c.admits(floor) {
			return semver{}, mvsConflict(module, reqs)
		}
	}
	return floor, nil
}

// lowestSatisfying returns the smallest candidate that admits every requirement
// — the MVS pick (admits-all is max-of-floors ∩ under every ceiling).
func lowestSatisfying(candidates []semver, reqs []requirement) (semver, bool) {
	var best semver
	found := false
	for _, c := range candidates {
		ok := true
		for _, r := range reqs {
			if !r.c.admits(c) {
				ok = false
				break
			}
		}
		if !ok {
			continue
		}
		if !found || c.compare(best) < 0 {
			best, found = c, true
		}
	}
	return best, found
}

// maxFloor is the maximum of the requirements' inclusive lower bounds.
func maxFloor(reqs []requirement) (semver, bool) {
	var floor semver
	have := false
	for _, r := range reqs {
		if lo, ok := r.c.floor(); ok {
			if !have || lo.compare(floor) > 0 {
				floor, have = lo, true
			}
		}
	}
	return floor, have
}

// mvsConflict builds the E0122 message: the constraints cannot be jointly
// satisfied, named by requirer, pointing at `aril upgrade`.
func mvsConflict(module string, reqs []requirement) error {
	var terms []string
	for _, r := range reqs {
		terms = append(terms, fmt.Sprintf("%q (required by %s)", r.c.text, requirerName(r.by)))
	}
	return fmt.Errorf(
		"aril: error[E0122]: dependency %q cannot be resolved: no version satisfies all constraints [%s]. "+
			"Raise a floor with `aril upgrade %s` — under minimal version selection a higher intermediate version may reconcile the constraints (the manual backtracking substitute).",
		module, strings.Join(terms, ", "), module)
}

func requirerName(by string) string {
	if by == "" {
		return "the root project"
	}
	return fmt.Sprintf("module %q", by)
}
