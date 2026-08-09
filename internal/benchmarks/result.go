package benchmarks

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
)

// Artifact is a durable execution artifact referenced from a benchmark result.
// Content stays in the artifact backend; PostgreSQL stores this manifest only.
type Artifact struct {
	Kind        string `json:"kind"`
	URI         string `json:"uri"`
	ContentType string `json:"content_type,omitempty"`
	SHA256      string `json:"sha256,omitempty"`
	SizeBytes   int64  `json:"size_bytes,omitempty"`
}

// ResultDetails is the benchmark-specific section of eval_case_results.details.
// Valid distinguishes a completed agent attempt from an infrastructure-invalid
// trial. OfficialMetrics intentionally preserves each benchmark's own metric
// semantics instead of forcing incomparable scores into a platform total.
type ResultDetails struct {
	Valid            *bool           `json:"valid,omitempty"`
	ErrorType        string          `json:"error_type,omitempty"`
	OfficialMetrics  json.RawMessage `json:"official_metrics,omitempty"`
	ArtifactManifest []Artifact      `json:"artifact_manifest,omitempty"`
}

// ValidateResultDetails validates benchmark outcome metadata before it is
// embedded in the existing generic details JSON column.
func ValidateResultDetails(details *ResultDetails) error {
	if details == nil {
		return nil
	}
	if details.ErrorType != "" && !identifierPattern.MatchString(details.ErrorType) {
		return errors.New("error_type must be a lowercase identifier")
	}
	if _, err := NormalizeJSONObject(details.OfficialMetrics, "official_metrics"); err != nil {
		return err
	}
	for index, artifact := range details.ArtifactManifest {
		if !identifierPattern.MatchString(artifact.Kind) {
			return fmt.Errorf("artifact_manifest[%d].kind must be a lowercase identifier", index)
		}
		if strings.TrimSpace(artifact.URI) == "" {
			return fmt.Errorf("artifact_manifest[%d].uri is required", index)
		}
		if artifact.SizeBytes < 0 {
			return fmt.Errorf("artifact_manifest[%d].size_bytes must not be negative", index)
		}
	}
	return nil
}

// EmbedResultDetails preserves generic runner details while placing validated
// benchmark data under the stable details.benchmark namespace.
func EmbedResultDetails(details json.RawMessage, benchmark *ResultDetails) (json.RawMessage, error) {
	if benchmark == nil {
		if len(bytes.TrimSpace(details)) == 0 {
			return json.RawMessage(`{}`), nil
		}
		return NormalizeJSONObject(details, "details")
	}
	if err := ValidateResultDetails(benchmark); err != nil {
		return nil, err
	}

	base, err := NormalizeJSONObject(details, "details")
	if err != nil {
		return nil, err
	}
	var envelope map[string]json.RawMessage
	if err := json.Unmarshal(base, &envelope); err != nil {
		return nil, fmt.Errorf("decode details: %w", err)
	}
	encoded, err := json.Marshal(benchmark)
	if err != nil {
		return nil, fmt.Errorf("encode benchmark details: %w", err)
	}
	envelope["benchmark"] = encoded
	return json.Marshal(envelope)
}

// ExtractResultDetails returns benchmark metadata from a persisted generic
// details object. Legacy results simply return nil.
func ExtractResultDetails(details json.RawMessage) (*ResultDetails, error) {
	if len(bytes.TrimSpace(details)) == 0 {
		return nil, nil
	}
	var envelope map[string]json.RawMessage
	if err := json.Unmarshal(details, &envelope); err != nil {
		return nil, fmt.Errorf("decode details: %w", err)
	}
	raw, ok := envelope["benchmark"]
	if !ok || len(raw) == 0 || string(raw) == "null" {
		return nil, nil
	}
	var result ResultDetails
	if err := json.Unmarshal(raw, &result); err != nil {
		return nil, fmt.Errorf("decode benchmark details: %w", err)
	}
	if err := ValidateResultDetails(&result); err != nil {
		return nil, err
	}
	return &result, nil
}
