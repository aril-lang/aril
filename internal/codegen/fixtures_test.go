package codegen

import (
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"strconv"
	"strings"
	"testing"

	"github.com/aril-lang/aril/internal/lexer"
	"github.com/aril-lang/aril/internal/parser"
)

// TestFixtures walks every *.txt manifest in ../../tests/codegen/,
// lexes + parses + emits Go for INPUT, and byte-compares the
// result against the GO section. STDOUT / EXIT sections are
// declared but executed in PR-D's integration runner.
func TestFixtures(t *testing.T) {
	root := filepath.Join("..", "..", "tests", "codegen")
	entries, err := os.ReadDir(root)
	if err != nil {
		t.Fatalf("read %s: %v", root, err)
	}
	for _, e := range entries {
		if e.IsDir() || !strings.HasSuffix(e.Name(), ".txt") {
			continue
		}
		name := e.Name()
		t.Run(name, func(t *testing.T) {
			path := filepath.Join(root, name)
			data, err := os.ReadFile(path)
			if err != nil {
				t.Fatalf("read %s: %v", path, err)
			}
			sections := parseManifest(string(data))
			input, ok := sections["INPUT"]
			if !ok {
				t.Fatalf("%s: missing INPUT section", name)
			}
			toks, lerr := lexer.Lex(input)
			if lerr != nil {
				t.Fatalf("%s: lex error: %v", name, lerr)
			}
			f, perr := parser.Parse(toks)
			if perr != nil {
				t.Fatalf("%s: parse error: %v", name, perr)
			}
			got, err := Emit(f, "")
			if err != nil {
				t.Fatalf("%s: emit error: %v", name, err)
			}
			want, ok := sections["GO"]
			if !ok {
				t.Fatalf("%s: missing GO section", name)
			}
			// UPDATE_CODEGEN_GOLDENS=1 rewrites the GO section in place
			// (Block R regeneration affordance) — splices the freshly
			// emitted Go into the manifest, leaving every other section
			// byte-identical. Off by default; never runs in CI.
			if os.Getenv("UPDATE_CODEGEN_GOLDENS") == "1" {
				if strings.TrimRight(got, "\n") != strings.TrimRight(want, "\n") {
					if err := rewriteGoSection(path, string(data), got); err != nil {
						t.Fatalf("%s: update golden: %v", name, err)
					}
					t.Logf("%s: GO section updated", name)
				}
				return
			}
			if strings.TrimRight(got, "\n") != strings.TrimRight(want, "\n") {
				t.Errorf("%s: GO mismatch\n--- got ---\n%s\n--- want ---\n%s",
					name, got, want)
			}

			// STDOUT / EXIT execution (optional sections). Only
			// runs when at least one is present; skips silently
			// when the `go` toolchain is missing.
			wantOut, hasOut := sections["STDOUT"]
			wantExit, hasExit := sections["EXIT"]
			if _, has := sections["STDERR"]; has {
				t.Fatalf("%s: STDERR section not yet supported by the fixture runner", name)
			}
			if !hasOut && !hasExit {
				return
			}
			if _, err := exec.LookPath("go"); err != nil {
				t.Skip("go toolchain not available; skip exec check")
			}
			dir := t.TempDir()
			if err := os.WriteFile(filepath.Join(dir, "main.go"), []byte(got), 0o644); err != nil {
				t.Fatalf("write: %v", err)
			}
			if err := os.WriteFile(filepath.Join(dir, "go.mod"),
				[]byte("module aril-fixture-exec\n\ngo 1.22\n"), 0o644); err != nil {
				t.Fatalf("write go.mod: %v", err)
			}
			cmd := exec.Command("go", "run", "./...")
			cmd.Dir = dir
			var outBuf, errBuf strings.Builder
			cmd.Stdout = &outBuf
			cmd.Stderr = &errBuf
			runErr := cmd.Run()
			gotOut := outBuf.String()
			gotExit := 0
			if ee, ok := runErr.(*exec.ExitError); ok {
				gotExit = ee.ExitCode()
			} else if runErr != nil {
				t.Fatalf("%s: go run failed unexpectedly: %v\nstderr:\n%s",
					name, runErr, errBuf.String())
			}
			if gotExit != 0 && errBuf.Len() > 0 {
				t.Logf("%s: go run stderr (exit %d):\n%s", name, gotExit, errBuf.String())
			}
			if hasOut {
				if strings.TrimRight(gotOut, "\n") != strings.TrimRight(wantOut, "\n") {
					t.Errorf("%s: STDOUT mismatch\n got: %q\nwant: %q",
						name, gotOut, wantOut)
				}
			}
			if hasExit {
				wantExitInt, err := strconv.Atoi(strings.TrimSpace(wantExit))
				if err != nil {
					t.Fatalf("%s: malformed EXIT section %q: %v", name, wantExit, err)
				}
				if gotExit != wantExitInt {
					t.Errorf("%s: exit code = %d; want %d", name, gotExit, wantExitInt)
				}
			}
		})
	}
}

// rewriteGoSection replaces the body of the `--- GO ---` section in the
// manifest at path with newGo, preserving every other section verbatim,
// and writes the file back. Used only under UPDATE_CODEGEN_GOLDENS.
func rewriteGoSection(path, orig, newGo string) error {
	delim := regexp.MustCompile(`(?m)^---\s+([A-Z_]+)\s+---\s*$`)
	matches := delim.FindAllStringSubmatchIndex(orig, -1)
	for i, m := range matches {
		if orig[m[2]:m[3]] != "GO" {
			continue
		}
		bodyStart := m[1] // end of the "--- GO ---" line
		bodyEnd := len(orig)
		if i+1 < len(matches) {
			bodyEnd = matches[i+1][0]
		}
		// The section body is "\n" + content + "\n\n" (blank line before
		// the next delimiter, or trailing newline at EOF). Normalise to
		// one leading newline + trimmed body + the original tail spacing.
		tail := "\n"
		if i+1 < len(matches) {
			tail = "\n\n"
		}
		rebuilt := orig[:bodyStart] + "\n" + strings.TrimRight(newGo, "\n") + tail + orig[bodyEnd:]
		return os.WriteFile(path, []byte(rebuilt), 0o644)
	}
	return os.WriteFile(path, []byte(orig), 0o644)
}

func parseManifest(s string) map[string]string {
	delim := regexp.MustCompile(`(?m)^---\s+([A-Z_]+)\s+---\s*$`)
	matches := delim.FindAllStringSubmatchIndex(s, -1)
	out := map[string]string{}
	for i, m := range matches {
		name := s[m[2]:m[3]]
		bodyStart := m[1]
		bodyEnd := len(s)
		if i+1 < len(matches) {
			bodyEnd = matches[i+1][0]
		}
		body := s[bodyStart:bodyEnd]
		body = strings.TrimPrefix(body, "\n")
		body = strings.TrimRight(body, "\n")
		out[name] = body
	}
	return out
}
