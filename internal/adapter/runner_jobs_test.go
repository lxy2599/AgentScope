package adapter

import "testing"

func TestNormalizeResultDetailsDefaultsEmptyJSON(t *testing.T) {
	got := normalizeResultDetails(nil)
	if string(got) != "{}" {
		t.Fatalf("normalizeResultDetails(nil) = %q, want {}", string(got))
	}

	got = normalizeResultDetails([]byte{})
	if string(got) != "{}" {
		t.Fatalf("normalizeResultDetails(empty) = %q, want {}", string(got))
	}
}

func TestNormalizeResultDetailsKeepsProvidedJSON(t *testing.T) {
	got := normalizeResultDetails([]byte(`{"score":1}`))
	if string(got) != `{"score":1}` {
		t.Fatalf("normalizeResultDetails(provided) = %q", string(got))
	}
}
