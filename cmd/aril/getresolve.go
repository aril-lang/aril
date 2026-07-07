package main

// getresolve.go — the MVS dependency-graph resolver behind `aril get`
// (RFC-0008 §Resolution / §Fetch). It enumerates each source's released tags
// (`git ls-remote --tags`), applies minimal version selection over the whole
// transitive graph (max-of-floors under every ceiling), fetches the selected
// versions into the hermetic cache, and returns the lock entries. Ranged
// constraints resolve here; exact tags and commit SHAs are the degenerate case
// (no enumeration). The network is touched only by tag enumeration + fetch —
// `aril build`/`run` read the resulting lock offline.

import (
	"fmt"
	"sort"
	"strings"
)

// resolveGraph performs MVS across root's transitive dependency graph and
// returns the lock entries (one per resolved module). The build list is grown
// to a fixpoint: a module is (re-)expanded at its currently-selected version,
// registering its own [dep] constraints, which may raise another module's
// selection and re-enqueue it. Selections only ever rise (MVS is monotone) over
// a finite tag set, so it terminates.
//
// Known limitation (fails closed, follow-up): constraints accumulate per source
// across *every* expanded version and are never retracted when a module's
// selection rises past an earlier-expanded version. In the rare graph where a
// superseded version imposed a now-irrelevant constraint on a shared module,
// that phantom constraint can force a *spurious* E0122 — a false conflict, never
// a wrong build, and dissolvable via the `aril upgrade` floor-raise (the
// accepted MVS-over-ranges incompleteness). Retracting superseded contributions
// (tag each requirement with its contributing source@version) is a PR5
// health-pass item.
func resolveGraph(root *projectManifest) ([]lockEntry, error) {
	prevResolved := map[string]string{} // source@version → commit, carried across a cache-hit
	if prev, err := readLock(root.dir); err == nil {
		for _, e := range prev {
			prevResolved[e.source+"@"+e.version] = e.resolved
		}
	}

	reqs := map[string][]requirement{} // source → all constraints in the build list
	tags := map[string][]semver{}      // source → enumerated semver tags (lazily, once)
	tagsDone := map[string]bool{}
	name := map[string]string{}     // source → an import-root name (first-seen, for messages/lock)
	sel := map[string]string{}      // source → the concrete selected version (tag or SHA)
	commitOf := map[string]string{} // source@version → the commit a fresh fetch resolved to
	expandedAt := map[string]string{}

	queue := []*projectManifest{root}
	for len(queue) > 0 {
		m := queue[0]
		queue = queue[1:]
		for i := range m.deps {
			d := &m.deps[i]
			// A `replace` dep is local — nothing to fetch. Every declared kind is
			// fetched into the cache (RFC-0010): kind="aril"/"binding" self-declare
			// an aril.toml (expanded below for its own [dep] constraints);
			// kind="go" is a raw Go module with none — a leaf of the Aril MVS
			// graph, fetched but not expanded (manifestAt returns nil below).
			if d.replace != "" {
				continue
			}
			cons, err := parseConstraint(d.version)
			if err != nil {
				return nil, fmt.Errorf("aril: [dep.%s] version: %v", d.name, err)
			}
			if name[d.source] == "" {
				name[d.source] = d.name
			}
			reqs[d.source] = append(reqs[d.source], requirement{by: m.name, c: cons})

			concrete, err := selectVersion(name[d.source], d.source, reqs[d.source], tags, tagsDone)
			if err != nil {
				return nil, err
			}
			if sel[d.source] == concrete {
				continue // no change — already expanded at this selection
			}
			sel[d.source] = concrete

			dest := cacheModuleDir(d.source, concrete)
			commit, err := ensureFetched(d.source, concrete, dest)
			if err != nil {
				return nil, err
			}
			if commit != "" {
				commitOf[d.source+"@"+concrete] = commit
			}
			// Expand the newly-selected version's manifest once (its [dep]
			// constraints may raise other modules).
			if expandedAt[d.source] != concrete {
				expandedAt[d.source] = concrete
				subM, err := manifestAt(dest)
				if err != nil {
					return nil, err
				}
				if subM != nil {
					if err := depKindGuard(d.name, d.kind, subM.packageKind); err != nil {
						return nil, err
					}
					// A published binding package (kind="binding") self-declares the
					// bound Go module it wraps; fetch that module too (RFC-0010), keyed
					// by its exact binds-go pin, so the build's require+replace finds
					// it offline. It is a Go module (no aril.toml) — a leaf.
					if subM.binds != "" && subM.bindsGo != "" {
						if name[subM.binds] == "" {
							name[subM.binds] = lastSegment(subM.binds)
						}
						if sel[subM.binds] != subM.bindsGo {
							sel[subM.binds] = subM.bindsGo
							bdest := cacheModuleDir(subM.binds, subM.bindsGo)
							bcommit, err := ensureFetched(subM.binds, subM.bindsGo, bdest)
							if err != nil {
								return nil, err
							}
							if bcommit != "" {
								commitOf[subM.binds+"@"+subM.bindsGo] = bcommit
							}
						}
					}
					queue = append(queue, subM)
				}
			}
		}
	}

	// Build the lock from the final selection. Sorted by name for a stable diff.
	sources := make([]string, 0, len(sel))
	for s := range sel {
		sources = append(sources, s)
	}
	sort.Strings(sources)
	var out []lockEntry
	for _, s := range sources {
		concrete := sel[s]
		dest := cacheModuleDir(s, concrete)
		commit := commitOf[s+"@"+concrete]
		if commit == "" {
			commit = prevResolved[s+"@"+concrete] // cache-hit: keep the recorded pin
		}
		hash, err := hashTree(dest)
		if err != nil {
			return nil, err
		}
		out = append(out, lockEntry{name: name[s], source: s, version: concrete, resolved: commit, hash: hash})
	}
	return out, nil
}

// selectVersion resolves the concrete version for one source given all its
// constraints: a commit SHA (exact identity), or a semver selection by MVS over
// the source's tags (plus the exact `=` versions as implicit candidates so a
// pure-exact-pin graph needs no tag enumeration). A conflict is E0122.
func selectVersion(modName, source string, rs []requirement, tags map[string][]semver, tagsDone map[string]bool) (string, error) {
	// Commit-SHA pins are exact identity, outside semver ordering.
	var shas []string
	hasRange := false
	var exactCands []semver
	for _, r := range rs {
		if r.c.commit != "" {
			shas = append(shas, r.c.commit)
			continue
		}
		if pin, ok := r.c.exactPin(); ok {
			v, err := parseSemver(pin)
			if err != nil {
				return "", fmt.Errorf("aril: dependency %q: %v", modName, err)
			}
			exactCands = append(exactCands, v)
		} else {
			hasRange = true
		}
	}
	if len(shas) > 0 {
		if hasRange || len(exactCands) > 0 || !allEqual(shas) {
			return "", fmt.Errorf("aril: error[E0122]: dependency %q pins a commit SHA that cannot be reconciled with the graph's other version requirements", modName)
		}
		return shas[0], nil
	}

	candidates := exactCands
	if hasRange {
		if !tagsDone[source] {
			ts, err := remoteTags(source)
			if err != nil {
				return "", err
			}
			tags[source] = ts
			tagsDone[source] = true
		}
		candidates = append(candidates, tags[source]...)
	}
	v, err := mvsSelect(modName, rs, candidates)
	if err != nil {
		return "", err
	}
	return v.String(), nil
}

func allEqual(ss []string) bool {
	for _, s := range ss {
		if s != ss[0] {
			return false
		}
	}
	return true
}

// remoteTags enumerates a source's released semver tags via `git ls-remote
// --tags` (RFC-0008 §Fetch — a repo's version tags are its release list).
// Non-semver tags and `^{}` peeled-tag lines are skipped.
func remoteTags(source string) ([]semver, error) {
	out, err := gitOutput("", "ls-remote", "--tags", gitURL(source))
	if err != nil {
		return nil, fmt.Errorf("aril get: enumerating tags of %q: %v", source, err)
	}
	var tags []semver
	for _, line := range strings.Split(out, "\n") {
		line = strings.TrimSpace(line)
		if line == "" {
			continue
		}
		fields := strings.Fields(line)
		if len(fields) != 2 {
			continue
		}
		ref := strings.TrimPrefix(fields[1], "refs/tags/")
		ref = strings.TrimSuffix(ref, "^{}") // skip annotated-tag peel lines
		v, err := parseSemver(ref)
		if err != nil {
			continue // not a vX.Y.Z release tag
		}
		tags = append(tags, v)
	}
	return tags, nil
}
