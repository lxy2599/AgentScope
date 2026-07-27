package adapter

import "testing"

func TestTryParseJSON(t *testing.T) {
	if got := string(tryParseJSON(`{"query":"hello"}`)); got != `{"query":"hello"}` {
		t.Fatalf("tryParseJSON() = %q", got)
	}
	if got := tryParseJSON("plain text"); got != nil {
		t.Fatalf("tryParseJSON(plain text) = %q, want nil", got)
	}
}

