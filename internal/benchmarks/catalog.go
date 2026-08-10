package benchmarks

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"regexp"
	"sort"
	"strings"
	"sync"
)

var (
	imageDigestPattern        = regexp.MustCompile(`^sha256:[a-f0-9]{64}$`)
	taskRunnerProtocolPattern = regexp.MustCompile(`^v[1-9][0-9]*$`)
	referenceIDPattern        = regexp.MustCompile(`^[A-Za-z0-9][A-Za-z0-9_.:-]{0,255}$`)
	secretValuePattern        = regexp.MustCompile(`(?i)(?:api[_-]?key|secret|password|authorization|bearer|token)\s*[:=]|^sk-[a-z0-9_-]{16,}$|^akia[0-9a-z]{16}$|^eyj[a-z0-9_-]{16,}\.[a-z0-9_-]+\.[a-z0-9_-]+$`)
)

// AgentArtifact identifies an immutable agent build that may be selected by a
// benchmark template. This is a read model only: catalog synchronization and
// artifact publishing deliberately live outside the AgentScope control plane.
type AgentArtifact struct {
	ID                 string   `json:"id"`
	Framework          string   `json:"framework"`
	Image              string   `json:"image"`
	ImageDigest        string   `json:"image_digest"`
	SourceCommit       string   `json:"source_commit,omitempty"`
	TaskRunnerProtocol string   `json:"task_runner_protocol"`
	Capabilities       []string `json:"capabilities,omitempty"`
	DisplayName        string   `json:"display_name,omitempty"`
}

// Profile is a versioned non-secret benchmark profile. Kind is one of model,
// agent, or execution. Config is intentionally opaque to AgentScope so the
// benchmark worker and framework adapter own framework-specific semantics.
type Profile struct {
	ID          string          `json:"id"`
	Kind        string          `json:"kind"`
	Framework   string          `json:"framework,omitempty"`
	Version     string          `json:"version,omitempty"`
	DisplayName string          `json:"display_name,omitempty"`
	Config      json.RawMessage `json:"config"`
}

// RunTemplate composes immutable catalog references into a selectable,
// reproducible benchmark launch input. The full validated catalog payload is
// persisted by the durable catalog store without changing this read API.
type RunTemplate struct {
	ID                 string          `json:"id"`
	DisplayName        string          `json:"display_name,omitempty"`
	SuiteID            string          `json:"suite_id"`
	AgentArtifactID    string          `json:"agent_artifact_id"`
	ModelProfileID     string          `json:"model_profile_id"`
	AgentProfileID     string          `json:"agent_profile_id"`
	ExecutionProfileID string          `json:"execution_profile_id"`
	TaskSelection      json.RawMessage `json:"task_selection"`
	ExecutionOverrides json.RawMessage `json:"execution_overrides,omitempty"`
	TrialsPerCase      int             `json:"trials_per_case"`
}

// Catalog is the minimal, project-scoped read model consumed by the API. Its
// Revision should be a Git commit or immutable catalog revision when a catalog
// synchronizer is introduced.
type Catalog struct {
	Revision     string          `json:"revision"`
	Artifacts    []AgentArtifact `json:"artifacts"`
	Profiles     []Profile       `json:"profiles"`
	RunTemplates []RunTemplate   `json:"run_templates"`
}

// TaskSelection is the normalized, non-secret task selector embedded in a run
// template. The external benchmarks repository validates the same contract;
// AgentScope repeats it before accepting a synced catalog so a Worker never
// receives an ambiguous selection policy.
type TaskSelection struct {
	Mode             string
	Include          []string
	Exclude          []string
	Count            int
	Seed             int64
	SourceExperiment string
}

// ResolvedRunTemplate is the immutable catalog composition selected for one
// benchmark experiment. Its values must be copied into the experiment/job
// execution configuration; workers must never resolve mutable catalog state.
type ResolvedRunTemplate struct {
	CatalogRevision  string
	Template         RunTemplate
	Artifact         AgentArtifact
	ModelProfile     Profile
	AgentProfile     Profile
	ExecutionProfile Profile
}

// ResolveRunTemplate validates a catalog then resolves every reference used by
// one run template. It also prevents accidental framework mismatches between an
// artifact and its selected agent profile.
func ResolveRunTemplate(catalog Catalog, templateID string) (ResolvedRunTemplate, error) {
	normalized, err := ValidateCatalog(catalog)
	if err != nil {
		return ResolvedRunTemplate{}, err
	}
	templateID = strings.TrimSpace(templateID)
	for _, template := range normalized.RunTemplates {
		if template.ID != templateID {
			continue
		}
		artifacts := make(map[string]AgentArtifact, len(normalized.Artifacts))
		for _, artifact := range normalized.Artifacts {
			artifacts[artifact.ID] = artifact
		}
		profiles := make(map[string]Profile, len(normalized.Profiles))
		for _, profile := range normalized.Profiles {
			profiles[profile.ID] = profile
		}
		artifact := artifacts[template.AgentArtifactID]
		agent := profiles[template.AgentProfileID]
		if artifact.Framework != agent.Framework {
			return ResolvedRunTemplate{}, fmt.Errorf("run template %q artifact and agent profile frameworks do not match", templateID)
		}
		return ResolvedRunTemplate{
			CatalogRevision:  normalized.Revision,
			Template:         template,
			Artifact:         artifact,
			ModelProfile:     profiles[template.ModelProfileID],
			AgentProfile:     agent,
			ExecutionProfile: profiles[template.ExecutionProfileID],
		}, nil
	}
	return ResolvedRunTemplate{}, fmt.Errorf("run template %q was not found", templateID)
}

// ParseTaskSelection parses and validates the versioned task-selection object.
func ParseTaskSelection(raw json.RawMessage) (TaskSelection, error) {
	var fields map[string]json.RawMessage
	if err := json.Unmarshal(raw, &fields); err != nil || fields == nil {
		return TaskSelection{}, errors.New("task_selection must be a JSON object")
	}
	decodeString := func(key string, required bool) (string, error) {
		value, found := fields[key]
		if !found {
			if required {
				return "", fmt.Errorf("task_selection.%s is required", key)
			}
			return "", nil
		}
		var parsed string
		if err := json.Unmarshal(value, &parsed); err != nil || strings.TrimSpace(parsed) == "" {
			return "", fmt.Errorf("task_selection.%s must be a non-empty string", key)
		}
		return parsed, nil
	}
	decodeStrings := func(key string, required bool) ([]string, error) {
		value, found := fields[key]
		if !found {
			if required {
				return nil, fmt.Errorf("task_selection.%s is required", key)
			}
			return nil, nil
		}
		var parsed []string
		if err := json.Unmarshal(value, &parsed); err != nil || (required && len(parsed) == 0) {
			return nil, fmt.Errorf("task_selection.%s must be a non-empty string list", key)
		}
		seen := make(map[string]struct{}, len(parsed))
		for _, item := range parsed {
			if strings.TrimSpace(item) == "" {
				return nil, fmt.Errorf("task_selection.%s must contain non-empty task ids", key)
			}
			if _, duplicate := seen[item]; duplicate {
				return nil, fmt.Errorf("task_selection.%s must not contain duplicate task ids", key)
			}
			seen[item] = struct{}{}
		}
		return parsed, nil
	}
	decodeInt := func(key string) (int64, error) {
		value, found := fields[key]
		if !found {
			return 0, fmt.Errorf("task_selection.%s is required", key)
		}
		var parsed int64
		if err := json.Unmarshal(value, &parsed); err != nil {
			return 0, fmt.Errorf("task_selection.%s must be an integer", key)
		}
		return parsed, nil
	}
	mode, err := decodeString("mode", true)
	if err != nil {
		return TaskSelection{}, err
	}
	allowed := map[string]map[string]struct{}{
		"all":               {"mode": {}},
		"ids":               {"mode": {}, "include": {}, "exclude": {}},
		"sample":            {"mode": {}, "count": {}, "seed": {}, "exclude": {}},
		"previous_failures": {"mode": {}, "source_experiment": {}},
	}
	allowedKeys, found := allowed[mode]
	if !found {
		return TaskSelection{}, errors.New("task_selection.mode must be all, ids, sample, or previous_failures")
	}
	for key := range fields {
		if _, ok := allowedKeys[key]; !ok {
			return TaskSelection{}, fmt.Errorf("task_selection.%s is not valid for mode %q", key, mode)
		}
	}
	selection := TaskSelection{Mode: mode}
	switch mode {
	case "ids":
		if selection.Include, err = decodeStrings("include", true); err != nil {
			return TaskSelection{}, err
		}
		if selection.Exclude, err = decodeStrings("exclude", false); err != nil {
			return TaskSelection{}, err
		}
	case "sample":
		count, countErr := decodeInt("count")
		if countErr != nil || count < 1 {
			return TaskSelection{}, errors.New("task_selection.count must be a positive integer")
		}
		seed, seedErr := decodeInt("seed")
		if seedErr != nil {
			return TaskSelection{}, seedErr
		}
		selection.Count, selection.Seed = int(count), seed
		if selection.Exclude, err = decodeStrings("exclude", false); err != nil {
			return TaskSelection{}, err
		}
	case "previous_failures":
		if selection.SourceExperiment, err = decodeString("source_experiment", true); err != nil {
			return TaskSelection{}, err
		}
	}
	return selection, nil
}

// CatalogReader exposes read operations for the catalog consumed by AgentScope.
type CatalogReader interface {
	ReadCatalog(ctx context.Context, projectID string) (Catalog, error)
}

// CatalogSyncer is the bounded catalog mutation boundary. A benchmarks CLI can
// sync validated Git/YAML content through this interface, but AgentScope never
// accepts API keys, model credentials, or arbitrary secret-bearing config.
type CatalogSyncer interface {
	SyncCatalog(ctx context.Context, projectID string, catalog Catalog) (Catalog, error)
}

// StaticCatalogReader is the backwards-compatible in-memory implementation
// used by focused handler tests and local fixtures. Production wiring uses
// PostgresCatalogStore; external callers retain the same reader/syncer API.
type StaticCatalogReader struct {
	mu       sync.RWMutex
	catalogs map[string]Catalog
}

func NewStaticCatalogReader() *StaticCatalogReader {
	return &StaticCatalogReader{catalogs: make(map[string]Catalog)}
}

// Set replaces a project catalog for composition roots and tests. External
// callers must use SyncCatalog, which validates the manifest before upserting.
func (r *StaticCatalogReader) Set(projectID string, catalog Catalog) {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.catalogs[strings.TrimSpace(projectID)] = cloneCatalog(catalog)
}

func (r *StaticCatalogReader) SyncCatalog(_ context.Context, projectID string, catalog Catalog) (Catalog, error) {
	projectID = strings.TrimSpace(projectID)
	if projectID == "" {
		return Catalog{}, errors.New("project_id is required")
	}
	normalized, err := ValidateCatalog(catalog)
	if err != nil {
		return Catalog{}, err
	}
	r.mu.Lock()
	r.catalogs[projectID] = cloneCatalog(normalized)
	r.mu.Unlock()
	return cloneCatalog(normalized), nil
}

func (r *StaticCatalogReader) ReadCatalog(_ context.Context, projectID string) (Catalog, error) {
	r.mu.RLock()
	catalog, found := r.catalogs[strings.TrimSpace(projectID)]
	r.mu.RUnlock()
	if !found {
		return unconfiguredCatalog(), nil
	}
	return cloneCatalog(catalog), nil
}

func unconfiguredCatalog() Catalog {
	return Catalog{
		Revision:     "unconfigured",
		Artifacts:    []AgentArtifact{},
		Profiles:     []Profile{},
		RunTemplates: []RunTemplate{},
	}
}

// ValidateCatalog validates an immutable benchmark catalog manifest and rejects
// secret-bearing keys and obvious credential values recursively. The returned
// catalog has compact JSON object fields and deterministic ordering.
func ValidateCatalog(catalog Catalog) (Catalog, error) {
	catalog.Revision = strings.TrimSpace(catalog.Revision)
	if catalog.Revision == "" || len(catalog.Revision) > 256 || containsSecretValue(catalog.Revision) {
		return Catalog{}, errors.New("revision is required and must not contain a secret")
	}
	catalog.Artifacts = append([]AgentArtifact(nil), catalog.Artifacts...)
	catalog.Profiles = append([]Profile(nil), catalog.Profiles...)
	catalog.RunTemplates = append([]RunTemplate(nil), catalog.RunTemplates...)
	if catalog.Artifacts == nil {
		catalog.Artifacts = []AgentArtifact{}
	}
	if catalog.Profiles == nil {
		catalog.Profiles = []Profile{}
	}
	if catalog.RunTemplates == nil {
		catalog.RunTemplates = []RunTemplate{}
	}

	artifactIDs := make(map[string]struct{}, len(catalog.Artifacts))
	for i := range catalog.Artifacts {
		artifact := &catalog.Artifacts[i]
		artifact.ID = strings.TrimSpace(artifact.ID)
		artifact.Framework = strings.TrimSpace(artifact.Framework)
		artifact.Image = strings.TrimSpace(artifact.Image)
		artifact.ImageDigest = strings.TrimSpace(artifact.ImageDigest)
		artifact.SourceCommit = strings.TrimSpace(artifact.SourceCommit)
		artifact.TaskRunnerProtocol = strings.TrimSpace(artifact.TaskRunnerProtocol)
		artifact.Capabilities = append([]string(nil), artifact.Capabilities...)
		artifact.DisplayName = strings.TrimSpace(artifact.DisplayName)
		if !identifierPattern.MatchString(artifact.ID) || !identifierPattern.MatchString(artifact.Framework) {
			return Catalog{}, fmt.Errorf("artifacts[%d] id and framework must be lowercase identifiers", i)
		}
		if _, exists := artifactIDs[artifact.ID]; exists {
			return Catalog{}, fmt.Errorf("duplicate artifact id %q", artifact.ID)
		}
		artifactIDs[artifact.ID] = struct{}{}
		if artifact.Image == "" || strings.ContainsAny(artifact.Image, " \t\n\r@") || containsSecretValue(artifact.Image) {
			return Catalog{}, fmt.Errorf("artifacts[%d].image must be a non-secret image reference without an embedded digest", i)
		}
		if !imageDigestPattern.MatchString(artifact.ImageDigest) {
			return Catalog{}, fmt.Errorf("artifacts[%d].image_digest must be an immutable sha256 digest", i)
		}
		if !taskRunnerProtocolPattern.MatchString(artifact.TaskRunnerProtocol) {
			return Catalog{}, fmt.Errorf("artifacts[%d].task_runner_protocol must be a version such as v1", i)
		}
		seenCapabilities := make(map[string]struct{}, len(artifact.Capabilities))
		for capabilityIndex, capability := range artifact.Capabilities {
			capability = strings.TrimSpace(capability)
			if !identifierPattern.MatchString(capability) {
				return Catalog{}, fmt.Errorf("artifacts[%d].capabilities[%d] must be a lowercase identifier", i, capabilityIndex)
			}
			if _, duplicate := seenCapabilities[capability]; duplicate {
				return Catalog{}, fmt.Errorf("artifacts[%d].capabilities contains duplicate %q", i, capability)
			}
			seenCapabilities[capability] = struct{}{}
			artifact.Capabilities[capabilityIndex] = capability
		}
		if containsSecretValue(artifact.SourceCommit) || containsSecretValue(artifact.DisplayName) {
			return Catalog{}, fmt.Errorf("artifacts[%d] contains a secret-like value", i)
		}
	}

	profiles := make(map[string]Profile, len(catalog.Profiles))
	for i := range catalog.Profiles {
		profile := &catalog.Profiles[i]
		profile.ID = strings.TrimSpace(profile.ID)
		profile.Kind = strings.TrimSpace(profile.Kind)
		profile.Framework = strings.TrimSpace(profile.Framework)
		profile.Version = strings.TrimSpace(profile.Version)
		profile.DisplayName = strings.TrimSpace(profile.DisplayName)
		if !identifierPattern.MatchString(profile.ID) {
			return Catalog{}, fmt.Errorf("profiles[%d].id must be a lowercase identifier", i)
		}
		if profile.Kind != "model" && profile.Kind != "agent" && profile.Kind != "execution" {
			return Catalog{}, fmt.Errorf("profiles[%d].kind must be model, agent, or execution", i)
		}
		if profile.Framework != "" && !identifierPattern.MatchString(profile.Framework) {
			return Catalog{}, fmt.Errorf("profiles[%d].framework must be a lowercase identifier", i)
		}
		if profile.Kind == "agent" && profile.Framework == "" {
			return Catalog{}, fmt.Errorf("profiles[%d].framework is required for agent profiles", i)
		}
		if _, exists := profiles[profile.ID]; exists {
			return Catalog{}, fmt.Errorf("duplicate profile id %q", profile.ID)
		}
		config, err := normalizeNonSecretJSONObject(profile.Config, fmt.Sprintf("profiles[%d].config", i))
		if err != nil {
			return Catalog{}, err
		}
		if containsSecretValue(profile.Version) || containsSecretValue(profile.DisplayName) {
			return Catalog{}, fmt.Errorf("profiles[%d] contains a secret-like value", i)
		}
		profile.Config = config
		profiles[profile.ID] = *profile
	}

	templateIDs := make(map[string]struct{}, len(catalog.RunTemplates))
	for i := range catalog.RunTemplates {
		template := &catalog.RunTemplates[i]
		template.ID = strings.TrimSpace(template.ID)
		template.DisplayName = strings.TrimSpace(template.DisplayName)
		template.SuiteID = strings.TrimSpace(template.SuiteID)
		template.AgentArtifactID = strings.TrimSpace(template.AgentArtifactID)
		template.ModelProfileID = strings.TrimSpace(template.ModelProfileID)
		template.AgentProfileID = strings.TrimSpace(template.AgentProfileID)
		template.ExecutionProfileID = strings.TrimSpace(template.ExecutionProfileID)
		if !identifierPattern.MatchString(template.ID) || !referenceIDPattern.MatchString(template.SuiteID) || !identifierPattern.MatchString(template.AgentArtifactID) || !identifierPattern.MatchString(template.ModelProfileID) || !identifierPattern.MatchString(template.AgentProfileID) || !identifierPattern.MatchString(template.ExecutionProfileID) {
			return Catalog{}, fmt.Errorf("run_templates[%d] contains an invalid identifier", i)
		}
		if _, exists := templateIDs[template.ID]; exists {
			return Catalog{}, fmt.Errorf("duplicate run template id %q", template.ID)
		}
		templateIDs[template.ID] = struct{}{}
		if _, exists := artifactIDs[template.AgentArtifactID]; !exists {
			return Catalog{}, fmt.Errorf("run_templates[%d] references unknown agent artifact", i)
		}
		if profile, exists := profiles[template.ModelProfileID]; !exists || profile.Kind != "model" {
			return Catalog{}, fmt.Errorf("run_templates[%d] references an unknown model profile", i)
		}
		if profile, exists := profiles[template.AgentProfileID]; !exists || profile.Kind != "agent" {
			return Catalog{}, fmt.Errorf("run_templates[%d] references an unknown agent profile", i)
		}
		if profile, exists := profiles[template.ExecutionProfileID]; !exists || profile.Kind != "execution" {
			return Catalog{}, fmt.Errorf("run_templates[%d] references an unknown execution profile", i)
		}
		if template.TrialsPerCase < 1 || template.TrialsPerCase > 10_000 {
			return Catalog{}, fmt.Errorf("run_templates[%d].trials_per_case must be between 1 and 10000", i)
		}
		selection, err := normalizeNonSecretJSONObject(template.TaskSelection, fmt.Sprintf("run_templates[%d].task_selection", i))
		if err != nil {
			return Catalog{}, err
		}
		if _, err := ParseTaskSelection(selection); err != nil {
			return Catalog{}, fmt.Errorf("run_templates[%d].task_selection is invalid: %w", i, err)
		}
		overrides := template.ExecutionOverrides
		if len(bytes.TrimSpace(overrides)) == 0 {
			overrides = json.RawMessage(`{}`)
		}
		overrides, err = normalizeNonSecretJSONObject(overrides, fmt.Sprintf("run_templates[%d].execution_overrides", i))
		if err != nil {
			return Catalog{}, err
		}
		if containsSecretValue(template.DisplayName) {
			return Catalog{}, fmt.Errorf("run_templates[%d] contains a secret-like value", i)
		}
		template.TaskSelection = selection
		template.ExecutionOverrides = overrides
	}
	return cloneCatalog(catalog), nil
}

func normalizeNonSecretJSONObject(raw json.RawMessage, field string) (json.RawMessage, error) {
	normalized, err := NormalizeJSONObject(raw, field)
	if err != nil {
		return nil, err
	}
	decoder := json.NewDecoder(bytes.NewReader(normalized))
	decoder.UseNumber()
	var value any
	if err := decoder.Decode(&value); err != nil {
		return nil, fmt.Errorf("decode %s: %w", field, err)
	}
	if err := rejectSecretValue(value); err != nil {
		return nil, fmt.Errorf("%s must not contain secrets: %w", field, err)
	}
	return normalized, nil
}

func rejectSecretValue(value any) error {
	switch typed := value.(type) {
	case map[string]any:
		for key, nested := range typed {
			if isSecretKey(key) {
				return fmt.Errorf("secret-like key %q is prohibited", key)
			}
			if err := rejectSecretValue(nested); err != nil {
				return err
			}
		}
	case []any:
		for _, nested := range typed {
			if err := rejectSecretValue(nested); err != nil {
				return err
			}
		}
	case string:
		if containsSecretValue(typed) {
			return errors.New("secret-like value is prohibited")
		}
	}
	return nil
}

func isSecretKey(key string) bool {
	normalized := strings.ToLower(strings.Map(func(r rune) rune {
		if (r >= 'a' && r <= 'z') || (r >= 'A' && r <= 'Z') || (r >= '0' && r <= '9') {
			return r
		}
		return -1
	}, key))
	if normalized == "apikey" || normalized == "secret" || normalized == "password" || normalized == "authorization" || normalized == "credential" || normalized == "privatekey" || normalized == "accesskey" || normalized == "token" || normalized == "bearer" {
		return true
	}
	return strings.HasSuffix(normalized, "apikey") || strings.HasSuffix(normalized, "secret") || strings.HasSuffix(normalized, "password") || strings.HasSuffix(normalized, "credential") || strings.HasSuffix(normalized, "privatekey") || strings.HasSuffix(normalized, "accesskey") || strings.HasSuffix(normalized, "token")
}

func containsSecretValue(value string) bool {
	return secretValuePattern.MatchString(value)
}

func cloneCatalog(catalog Catalog) Catalog {
	catalog.Artifacts = append([]AgentArtifact(nil), catalog.Artifacts...)
	for i := range catalog.Artifacts {
		catalog.Artifacts[i].Capabilities = append([]string(nil), catalog.Artifacts[i].Capabilities...)
	}
	catalog.Profiles = append([]Profile(nil), catalog.Profiles...)
	catalog.RunTemplates = append([]RunTemplate(nil), catalog.RunTemplates...)
	for i := range catalog.Profiles {
		catalog.Profiles[i].Config = append(json.RawMessage(nil), catalog.Profiles[i].Config...)
	}
	for i := range catalog.RunTemplates {
		catalog.RunTemplates[i].TaskSelection = append(json.RawMessage(nil), catalog.RunTemplates[i].TaskSelection...)
		catalog.RunTemplates[i].ExecutionOverrides = append(json.RawMessage(nil), catalog.RunTemplates[i].ExecutionOverrides...)
	}
	sort.Slice(catalog.Artifacts, func(i, j int) bool { return catalog.Artifacts[i].ID < catalog.Artifacts[j].ID })
	sort.Slice(catalog.Profiles, func(i, j int) bool { return catalog.Profiles[i].ID < catalog.Profiles[j].ID })
	sort.Slice(catalog.RunTemplates, func(i, j int) bool { return catalog.RunTemplates[i].ID < catalog.RunTemplates[j].ID })
	return catalog
}
