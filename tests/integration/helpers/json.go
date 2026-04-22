package helpers

import (
	"encoding/json"
	"testing"
)

func DecodeJSON[T any](t *testing.T, body []byte, out *T) {
	t.Helper()
	if err := json.Unmarshal(body, out); err != nil {
		t.Fatalf("failed to decode JSON response: %v body=%s", err, string(body))
	}
}
