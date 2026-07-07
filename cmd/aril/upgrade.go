package main

// upgrade.go — `aril upgrade [<dep>...]`, the explicit newest-compatible action
// (RFC-0008 §Resolution). Because MVS selects the *minimum* satisfying version,
// default builds stay minimal and reproducible; `aril upgrade` raises a ranged
// dependency's floor to the **highest tag within its constraint window** (Go's
// `-u`, wearing caret clothing) and re-locks. It is the manual substitute the
// E0122 conflict message points at.

import (
	"flag"
	"fmt"
	"os"
	"strings"
)

func cmdUpgrade(args []string) int {
	fs := flag.NewFlagSet("aril upgrade", flag.ContinueOnError)
	fs.SetOutput(os.Stderr)
	fs.Usage = func() {
		fmt.Fprintln(os.Stderr, "usage: aril upgrade [<dep>...]   (default: every ranged dependency)")
	}
	if err := fs.Parse(args); err != nil {
		return 2
	}
	only := map[string]bool{}
	for _, n := range fs.Args() {
		only[n] = true
	}

	cwd, err := os.Getwd()
	if err != nil {
		fmt.Fprintln(os.Stderr, "aril:", err)
		return 1
	}
	m, err := findProjectManifest(cwd)
	if err != nil {
		fmt.Fprintln(os.Stderr, err)
		return 1
	}
	if m == nil {
		fmt.Fprintln(os.Stderr, "aril upgrade: no aril.toml found")
		return 1
	}

	manifestPath := m.dir + string(os.PathSeparator) + "aril.toml"
	changed := 0
	for i := range m.deps {
		d := &m.deps[i]
		if d.replace != "" || (d.kind != "" && d.kind != "aril") {
			continue // local / Go-binding deps have no ranged tag window here
		}
		if len(only) > 0 && !only[d.name] {
			continue
		}
		cons, err := parseConstraint(d.version)
		if err != nil {
			fmt.Fprintf(os.Stderr, "aril upgrade: [dep.%s] version: %v\n", d.name, err)
			return 1
		}
		newText, ok := newFloorSpelling(cons)
		if !ok {
			if len(only) > 0 {
				fmt.Printf("aril: [dep.%s] %q is not a caret/tilde range — nothing to upgrade\n", d.name, d.version)
			}
			continue // exact pins, wildcards, SHAs: no floor to raise
		}
		tags, err := remoteTags(d.source)
		if err != nil {
			fmt.Fprintln(os.Stderr, err)
			return 1
		}
		highest, found := highestSatisfying(tags, cons)
		if !found {
			fmt.Printf("aril: [dep.%s] no released tag satisfies %q — left unchanged\n", d.name, d.version)
			continue
		}
		// Compare the *floor semver*, not the text: only a genuine raise
		// (highest strictly above the current floor) is an upgrade — a merely
		// re-spelled floor (`^1.5` with newest v1.5.0) is a no-op, so it must not
		// trigger a spurious re-lock or a misleading "upgraded" message.
		if curFloor, ok := cons.floor(); ok && highest.compare(curFloor) <= 0 {
			continue
		}
		raised := newText(highest)
		if raised == d.version {
			continue
		}
		if err := rewriteDepVersion(manifestPath, d.name, raised); err != nil {
			fmt.Fprintln(os.Stderr, err)
			return 1
		}
		fmt.Printf("aril: [dep.%s] %s → %s\n", d.name, d.version, raised)
		changed++
	}

	if changed == 0 {
		fmt.Println("aril: all dependencies already at their newest-compatible floor")
		return 0
	}
	// Re-resolve against the raised floors and rewrite the lock.
	updated, err := parseProjectManifest(manifestPath)
	if err != nil {
		fmt.Fprintln(os.Stderr, err)
		return 1
	}
	entries, err := resolveGraph(updated)
	if err != nil {
		fmt.Fprintln(os.Stderr, err)
		return 1
	}
	if err := writeLock(updated.dir, entries); err != nil {
		fmt.Fprintln(os.Stderr, err)
		return 1
	}
	fmt.Printf("aril: upgraded %d dependency floor(s); re-resolved and wrote %s\n", changed, lockFileName)
	return 0
}

// newFloorSpelling returns a function that renders the constraint's spelling
// with its floor raised to a given version, or false when the constraint has no
// raisable floor (exact pin, wildcard, compound, or SHA). Only caret/tilde
// ranges are upgraded — they carry a single floor that newest-compatible raises.
func newFloorSpelling(c constraint) (func(semver) string, bool) {
	switch {
	case strings.HasPrefix(c.text, "^"):
		return func(v semver) string { return "^" + versionCore(v) }, true
	case strings.HasPrefix(c.text, "~"):
		return func(v semver) string { return "~" + versionCore(v) }, true
	default:
		return nil, false
	}
}

// versionCore renders a semver without the leading `v` (the caret/tilde
// constraint spelling: `^1.5.2`, not `^v1.5.2`).
func versionCore(v semver) string {
	return strings.TrimPrefix(v.String(), "v")
}

// rewriteDepVersion replaces the `version = "..."` line inside the target
// `[dep.<name>]` section of an aril.toml, preserving every other line (comments,
// layout, other sections) — a targeted closed-schema edit, not a re-serialise.
func rewriteDepVersion(path, depName, newVersion string) error {
	data, err := os.ReadFile(path)
	if err != nil {
		return fmt.Errorf("aril upgrade: %w", err)
	}
	lines := strings.Split(string(data), "\n")
	inTarget := false
	done := false
	for i, raw := range lines {
		line := strings.TrimSpace(stripComment(raw))
		if strings.HasPrefix(line, "[") && strings.HasSuffix(line, "]") {
			section := strings.TrimSpace(line[1 : len(line)-1])
			inTarget = section == "dep."+depName
			continue
		}
		if inTarget && !done {
			key, _, ok := strings.Cut(line, "=")
			if ok && strings.TrimSpace(key) == "version" {
				// Preserve leading indentation; replace the value.
				indent := raw[:len(raw)-len(strings.TrimLeft(raw, " \t"))]
				lines[i] = fmt.Sprintf("%sversion = \"%s\"", indent, newVersion)
				done = true
			}
		}
	}
	if !done {
		return fmt.Errorf("aril upgrade: could not find a version line in [dep.%s]", depName)
	}
	return os.WriteFile(path, []byte(strings.Join(lines, "\n")), 0o644)
}
