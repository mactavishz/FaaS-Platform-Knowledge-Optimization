package helpers

import (
	"os"
	"path/filepath"
	"strings"
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

func RequireWorkflowEnvFile(t *testing.T, relPath string) {
	t.Helper()
	absPath := filepath.Join(RepoRoot(t), relPath)

	content, err := os.ReadFile(absPath)
	if err != nil {
		t.Fatalf("required workflow env file missing or unreadable: %s err=%v", absPath, err)
	}

	text := string(content)
	if strings.TrimSpace(text) == "" {
		t.Fatalf("required workflow env file is empty: %s", absPath)
	}

	if !strings.Contains(text, "SUPABASE_URL") || !strings.Contains(text, "SUPABASE_KEY") {
		t.Fatalf("required workflow env file must define SUPABASE_URL and SUPABASE_KEY: %s", absPath)
	}

	if strings.Contains(text, "your_supabase_project_url") || strings.Contains(text, "your_supabase_publishable_key") {
		t.Fatalf("required workflow env file still contains placeholder values: %s", absPath)
	}
}
