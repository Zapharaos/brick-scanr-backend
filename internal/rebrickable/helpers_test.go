package rebrickable

import (
	"encoding/json"
	"testing"
)

// mustUnmarshal decodes JSON into v, failing the test on error.
func mustUnmarshal(t *testing.T, data string, v interface{}) {
	t.Helper()
	if err := json.Unmarshal([]byte(data), v); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
}
