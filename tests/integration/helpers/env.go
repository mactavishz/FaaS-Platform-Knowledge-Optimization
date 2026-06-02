package helpers

import (
	"os"
	"strings"
	"testing"
)

func RequireWorkflowSupabaseEnv(t *testing.T) {
	t.Helper()

	if strings.TrimSpace(os.Getenv("SUPABASE_URL")) == "" || strings.TrimSpace(os.Getenv("SUPABASE_KEY")) == "" {
		t.Fatal("Supabase-backed workflow tests require SUPABASE_URL and SUPABASE_KEY to be set in the current environment")
	}
}
