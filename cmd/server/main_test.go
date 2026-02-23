package main

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestBuildRuntimePathDeduplicatesAndAddsHomeBins(t *testing.T) {
	t.Parallel()

	existing := t.TempDir()
	home := t.TempDir()
	localBin := filepath.Join(home, ".local", "bin")
	cargoBin := filepath.Join(home, ".cargo", "bin")
	if err := os.MkdirAll(localBin, 0o755); err != nil {
		t.Fatalf("mkdir local bin: %v", err)
	}
	if err := os.MkdirAll(cargoBin, 0o755); err != nil {
		t.Fatalf("mkdir cargo bin: %v", err)
	}

	missing := filepath.Join(home, "missing-bin")
	current := strings.Join([]string{existing, existing, missing}, string(os.PathListSeparator))

	got := buildRuntimePath(current, home)
	parts := filepath.SplitList(got)

	if countPath(parts, existing) != 1 {
		t.Fatalf("expected %q once in PATH, got %v", existing, parts)
	}
	if !containsPath(parts, localBin) {
		t.Fatalf("expected PATH to include %q, got %v", localBin, parts)
	}
	if !containsPath(parts, cargoBin) {
		t.Fatalf("expected PATH to include %q, got %v", cargoBin, parts)
	}
	if containsPath(parts, missing) {
		t.Fatalf("did not expect missing path %q in PATH, got %v", missing, parts)
	}
}

func TestPythonShimContentUsesUVFirst(t *testing.T) {
	t.Parallel()

	content := pythonShimContent()
	required := []string{
		"exec uv run python \"$@\"",
		"if command -v python3 >/dev/null 2>&1; then",
		"python is unavailable: neither uv nor python3 were found in PATH",
	}
	for _, fragment := range required {
		if !strings.Contains(content, fragment) {
			t.Fatalf("pythonShimContent missing %q", fragment)
		}
	}
}

func containsPath(paths []string, target string) bool {
	for _, path := range paths {
		if filepath.Clean(path) == filepath.Clean(target) {
			return true
		}
	}
	return false
}

func countPath(paths []string, target string) int {
	count := 0
	for _, path := range paths {
		if filepath.Clean(path) == filepath.Clean(target) {
			count++
		}
	}
	return count
}
