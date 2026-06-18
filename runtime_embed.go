// Package aril is the module root. Its sole job is to embed the arilrt
// runtime sources so the compiler binary carries the exact runtime it
// emits — the basis of vendored-mode builds (D18 CT2: the runtime a
// program links against is version-locked to the compiler that produced
// it, by construction).
package aril

import (
	"embed"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"strings"
)

// runtimeFS holds the arilrt package sources, embedded at compiler build
// time. Test files are embedded too (the glob is coarse) but are skipped
// when vendoring — `go run`/`go build` of the emitted program ignores
// _test.go regardless.
//
//go:embed arilrt/*.go
var runtimeFS embed.FS

// WriteVendoredRuntime copies the embedded arilrt sources into
// <destDir>/arilrt as a self-contained subpackage of the temp build
// module, so emitted code can `import "<module>/arilrt"`. Test files are
// omitted. Returns the number of files written.
func WriteVendoredRuntime(destDir string) (int, error) {
	pkgDir := filepath.Join(destDir, "arilrt")
	if err := os.MkdirAll(pkgDir, 0o755); err != nil {
		return 0, fmt.Errorf("arilrt vendor: mkdir: %w", err)
	}
	entries, err := fs.ReadDir(runtimeFS, "arilrt")
	if err != nil {
		return 0, fmt.Errorf("arilrt vendor: read embedded fs: %w", err)
	}
	n := 0
	for _, e := range entries {
		name := e.Name()
		if e.IsDir() || strings.HasSuffix(name, "_test.go") {
			continue
		}
		data, err := runtimeFS.ReadFile("arilrt/" + name)
		if err != nil {
			return n, fmt.Errorf("arilrt vendor: read %s: %w", name, err)
		}
		if err := os.WriteFile(filepath.Join(pkgDir, name), data, 0o644); err != nil {
			return n, fmt.Errorf("arilrt vendor: write %s: %w", name, err)
		}
		n++
	}
	return n, nil
}
