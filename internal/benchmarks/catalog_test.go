package benchmarks

import (
	"context"
	"encoding/json"
	"strings"
	"testing"
)

func validCatalog() Catalog {
	return Catalog{
		Revision: "git:abc123",
		Artifacts: []AgentArtifact{{
			ID: "openhands-dev", Framework: "openhands", Image: "local/openhands:dev",
			ImageDigest: "sha256:" + strings.Repeat("a", 64), TaskRunnerProtocol: "v1",
		}},
		Profiles: []Profile{
			{ID: "model-fixed", Kind: "model", Config: json.RawMessage(`{"model":"openai/example","max_tokens":8192}`)},
			{ID: "openhands-default", Kind: "agent", Framework: "openhands", Config: json.RawMessage(`{"max_iterations":50}`)},
			{ID: "smoke", Kind: "execution", Config: json.RawMessage(`{"concurrency":1}`)},
		},
		RunTemplates: []RunTemplate{{
			ID: "openhands-smoke", SuiteID: "terminal-bench-2.0", AgentArtifactID: "openhands-dev",
			ModelProfileID: "model-fixed", AgentProfileID: "openhands-default", ExecutionProfileID: "smoke",
			TaskSelection: json.RawMessage(`{"mode":"sample","count":1,"seed":42}`), TrialsPerCase: 1,
		}},
	}
}

func TestStaticCatalogReaderSyncsValidatedManifest(t *testing.T) {
	reader := NewStaticCatalogReader()
	catalog, err := reader.SyncCatalog(context.Background(), "project-1", validCatalog())
	if err != nil {
		t.Fatalf("SyncCatalog() error = %v", err)
	}
	if catalog.Revision != "git:abc123" || len(catalog.Artifacts) != 1 || len(catalog.Profiles) != 3 || len(catalog.RunTemplates) != 1 {
		t.Fatalf("synced catalog = %#v", catalog)
	}
	stored, err := reader.ReadCatalog(context.Background(), "project-1")
	if err != nil {
		t.Fatalf("ReadCatalog() error = %v", err)
	}
	if stored.Revision != catalog.Revision || string(stored.Profiles[0].Config) == "" {
		t.Fatalf("stored catalog = %#v", stored)
	}
}

func TestCatalogValidationRejectsSecretBearingManifest(t *testing.T) {
	catalog := validCatalog()
	catalog.Profiles[0].Config = json.RawMessage(`{"api_key":"do-not-accept"}`)
	if _, err := ValidateCatalog(catalog); err == nil || !strings.Contains(err.Error(), "secrets") {
		t.Fatalf("ValidateCatalog() error = %v, want secret rejection", err)
	}

	catalog = validCatalog()
	catalog.RunTemplates[0].TaskSelection = json.RawMessage(`{"headers":{"Authorization":"Bearer do-not-accept"}}`)
	if _, err := ValidateCatalog(catalog); err == nil || !strings.Contains(err.Error(), "secrets") {
		t.Fatalf("ValidateCatalog() nested secret error = %v, want secret rejection", err)
	}

	catalog = validCatalog()
	catalog.Profiles[1].Config = json.RawMessage(`{"AGENTSCOPE_PROJECT_TOKEN":"do-not-accept"}`)
	if _, err := ValidateCatalog(catalog); err == nil || !strings.Contains(err.Error(), "secrets") {
		t.Fatalf("ValidateCatalog() project token error = %v, want secret rejection", err)
	}
}
