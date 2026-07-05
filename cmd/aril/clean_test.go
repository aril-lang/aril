package main

import (
	"os"
	"path/filepath"
	"testing"
)

// TestCleanSelectors checks `aril clean` removes the whole out-dir by default
// and only the named sub-tree with --gen / --bin (RFC-0009 §Cleaning). Uses the
// default (non-shared) out-dir so no project-id segment is interposed.
func TestCleanSelectors(t *testing.T) {
	t.Setenv("ARIL_OUT", "")

	// mkdirs a fresh aril-out/{gen,bin} under a temp project and returns paths.
	setup := func(t *testing.T) (proj, outDir, gen, bin string) {
		proj = t.TempDir()
		writeFile(t, proj, "main.aril", "func main() {}\n")
		outDir = filepath.Join(proj, "aril-out")
		gen = filepath.Join(outDir, "gen")
		bin = filepath.Join(outDir, "bin")
		for _, d := range []string{gen, bin} {
			if err := os.MkdirAll(d, 0o755); err != nil {
				t.Fatal(err)
			}
			if err := os.WriteFile(filepath.Join(d, "marker"), []byte("x"), 0o644); err != nil {
				t.Fatal(err)
			}
		}
		return
	}
	exists := func(p string) bool { _, err := os.Stat(p); return err == nil }

	t.Run("gen-only", func(t *testing.T) {
		proj, _, gen, bin := setup(t)
		if code := cmdClean([]string{"-gen", proj}); code != 0 {
			t.Fatalf("clean -gen exit %d", code)
		}
		if exists(gen) {
			t.Error("gen/ not removed")
		}
		if !exists(bin) {
			t.Error("bin/ wrongly removed by -gen")
		}
	})

	t.Run("bin-only", func(t *testing.T) {
		proj, _, gen, bin := setup(t)
		if code := cmdClean([]string{"-bin", proj}); code != 0 {
			t.Fatalf("clean -bin exit %d", code)
		}
		if exists(bin) {
			t.Error("bin/ not removed")
		}
		if !exists(gen) {
			t.Error("gen/ wrongly removed by -bin")
		}
	})

	t.Run("whole", func(t *testing.T) {
		proj, outDir, _, _ := setup(t)
		if code := cmdClean([]string{proj}); code != 0 {
			t.Fatalf("clean exit %d", code)
		}
		if exists(outDir) {
			t.Error("aril-out/ not removed")
		}
	})
}

// TestCleanIdempotent: cleaning a project with no out-dir is a success, not an
// error (RemoveAll on an absent path is a no-op).
func TestCleanIdempotent(t *testing.T) {
	t.Setenv("ARIL_OUT", "")
	proj := t.TempDir()
	writeFile(t, proj, "main.aril", "func main() {}\n")
	if code := cmdClean([]string{proj}); code != 0 {
		t.Fatalf("clean of empty project exit %d, want 0", code)
	}
}

// TestCleanRefusesProjectRoot: a `[build] out-dir = "."`/".." typo must not let
// clean delete the project root or its parent — the guard rejects (exit 1) and
// leaves the source in place.
func TestCleanRefusesProjectRoot(t *testing.T) {
	t.Setenv("ARIL_OUT", "")
	for _, badOutDir := range []string{".", ".."} {
		t.Run("out-dir="+badOutDir, func(t *testing.T) {
			parent := t.TempDir()
			proj := filepath.Join(parent, "proj")
			if err := os.MkdirAll(proj, 0o755); err != nil {
				t.Fatal(err)
			}
			writeFile(t, proj, "main.aril", "func main() {}\n")
			writeFile(t, proj, "aril.toml", "[project]\nname=\"p\"\n\n[build]\nout-dir = \""+badOutDir+"\"\n")
			if code := cmdClean([]string{proj}); code != 1 {
				t.Fatalf("clean should refuse (exit 1), got %d", code)
			}
			// The source survived.
			if _, err := os.Stat(filepath.Join(proj, "main.aril")); err != nil {
				t.Errorf("source removed by refused clean: %v", err)
			}
		})
	}
}

// TestCleanSharedSegmentOnly: cleaning a project pointed at a shared out-dir
// removes only its own <project-id> segment, never a sibling's.
func TestCleanSharedSegmentOnly(t *testing.T) {
	shared := t.TempDir()

	// Build two sibling projects into the one shared out-dir.
	pA, pB := t.TempDir(), t.TempDir()
	writeFile(t, pA, "main.aril", "func main() {}\n")
	writeFile(t, pB, "main.aril", "func main() {}\n")
	dirA, _ := resolveOutDir(filepath.Join(pA, "main.aril"), shared)
	dirB, _ := resolveOutDir(filepath.Join(pB, "main.aril"), shared)
	for _, d := range []string{dirA, dirB} {
		if err := os.MkdirAll(filepath.Join(d, "bin"), 0o755); err != nil {
			t.Fatal(err)
		}
	}

	if code := cmdClean([]string{"-out-dir", shared, filepath.Join(pA, "main.aril")}); code != 0 {
		t.Fatalf("clean exit %d", code)
	}
	if _, err := os.Stat(dirA); !os.IsNotExist(err) {
		t.Errorf("project A's segment not removed (err=%v)", err)
	}
	if _, err := os.Stat(dirB); err != nil {
		t.Errorf("sibling B's segment wrongly removed: %v", err)
	}
}
