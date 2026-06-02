package helpers

import (
	"os"
	"path/filepath"
	"testing"
)

func RepoRoot(t *testing.T) string {
	t.Helper()
	dir, err := os.Getwd()
	if err != nil {
		t.Fatalf("failed to get current working directory: %v", err)
	}

	for {
		if _, err := os.Stat(filepath.Join(dir, "Vagrantfile")); err == nil {
			return dir
		}
		parent := filepath.Dir(dir)
		if parent == dir {
			t.Fatalf("failed to locate repository root from %s", dir)
		}
		dir = parent
	}
}
