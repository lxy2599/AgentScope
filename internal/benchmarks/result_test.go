package benchmarks

import (
	"encoding/json"
	"testing"
)

func TestEmbedAndExtractResultDetails(t *testing.T) {
	valid := true
	merged, err := EmbedResultDetails(json.RawMessage(`{"runner":{"steps":3}}`), &ResultDetails{
		Valid: validPtr(valid), ErrorType: "target_timeout", OfficialMetrics: json.RawMessage(`{"reward":0}`),
		ArtifactManifest: []Artifact{{Kind: "verifier_report", URI: "file:///tmp/report.json", SizeBytes: 10}},
	})
	if err != nil {
		t.Fatalf("EmbedResultDetails() error = %v", err)
	}
	if string(merged) == `{"benchmark":{}}` {
		t.Fatalf("merged details lost generic fields: %s", merged)
	}
	got, err := ExtractResultDetails(merged)
	if err != nil {
		t.Fatalf("ExtractResultDetails() error = %v", err)
	}
	if got == nil || got.Valid == nil || !*got.Valid || got.ErrorType != "target_timeout" {
		t.Fatalf("details = %#v", got)
	}
}

func TestValidateResultDetailsRejectsInvalidArtifact(t *testing.T) {
	err := ValidateResultDetails(&ResultDetails{ArtifactManifest: []Artifact{{Kind: "report", URI: "", SizeBytes: 1}}})
	if err == nil {
		t.Fatal("ValidateResultDetails() error = nil")
	}
}

func validPtr(value bool) *bool { return &value }
