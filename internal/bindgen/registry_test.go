package bindgen

import (
	"flag"
	"os"
	"path/filepath"
	"testing"
)

var update = flag.Bool("update", false, "regenerate internal/binding/registry_gen.go from the Manifest")

// registryGenPath is internal/binding/registry_gen.go relative to this test.
const registryGenPath = "../binding/registry_gen.go"

// TestRegistryRegen is the drift guard for the derived stdlib registry: the
// committed registry_gen.go must equal a fresh derivation from the Manifest.
// `-update` rewrites the file (the regeneration entry point). Skips when the Go
// source tree is unavailable (go/importer source mode needs GOROOT/src), the
// same fallback the rest of bindgen's tests use.
func TestRegistryRegen(t *testing.T) {
	src, err := RenderRegistry()
	if err != nil {
		t.Skipf("registry derivation unavailable (no Go source tree?): %v", err)
	}
	if *update {
		if err := os.WriteFile(registryGenPath, []byte(src), 0o644); err != nil {
			t.Fatalf("write %s: %v", registryGenPath, err)
		}
		t.Logf("regenerated %s", registryGenPath)
		return
	}
	committed, err := os.ReadFile(filepath.Clean(registryGenPath))
	if err != nil {
		t.Fatalf("read committed registry: %v", err)
	}
	if string(committed) != src {
		t.Errorf("internal/binding/registry_gen.go is stale — regenerate with\n" +
			"  go test ./internal/bindgen -run TestRegistryRegen -update\n" +
			"committed and derived differ")
	}
}
