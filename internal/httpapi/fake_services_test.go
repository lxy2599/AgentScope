package httpapi

import (
	"context"
	"encoding/json"
	"errors"
	"strings"
	"time"

	"AgentScope/internal/auth"
	"AgentScope/internal/evals"
	"AgentScope/internal/experiments"
	"AgentScope/internal/projects"
	"AgentScope/internal/sampling"
)

// ---- Fake Auth Service ----

type fakeAuthService struct {
	users        map[string]string // email -> userID
	passwords    map[string]string // userID -> password hash (password + ":hashed")
	displayNames map[string]string // userID -> display name
	counter      int
}

func newFakeAuthService() *fakeAuthService {
	return &fakeAuthService{
		users:        make(map[string]string),
		passwords:    make(map[string]string),
		displayNames: make(map[string]string),
	}
}

func (f *fakeAuthService) Register(ctx context.Context, email, password, displayName string) (string, error) {
	if email == "" || password == "" {
		return "", errors.New("email and password required")
	}
	if _, exists := f.users[email]; exists {
		return "", auth.ErrEmailAlreadyRegistered
	}
	f.counter++
	userID := "user-" + itoa(f.counter)
	f.users[email] = userID
	f.passwords[userID] = password + ":hashed"
	f.displayNames[userID] = displayName
	return userID, nil
}

func (f *fakeAuthService) Login(ctx context.Context, email, password string) (string, string, error) {
	userID, exists := f.users[email]
	if !exists {
		return "", "", auth.ErrInvalidCredentials
	}
	if f.passwords[userID] != password+":hashed" {
		return "", "", auth.ErrInvalidCredentials
	}
	return userID, f.displayNames[userID], nil
}

// ---- Fake Projects Service ----

type fakeProjectsService struct {
	projects    map[string]Project           // projectID -> project
	members     map[string]map[string]string // projectID -> userID -> role
	tokens      map[string]ProjectToken
	projCounter int
	tokCounter  int
}

func newFakeProjectsService() *fakeProjectsService {
	return &fakeProjectsService{
		projects: make(map[string]Project),
		members:  make(map[string]map[string]string),
		tokens:   make(map[string]ProjectToken),
	}
}

func (f *fakeProjectsService) CreateProject(ctx context.Context, userID, name, description string) (Project, error) {
	f.projCounter++
	id := "proj-" + itoa(f.projCounter)
	p := Project{
		ID:          id,
		Name:        name,
		Description: description,
	}
	f.projects[id] = p
	if f.members[id] == nil {
		f.members[id] = make(map[string]string)
	}
	f.members[id][userID] = "owner"
	return p, nil
}

func (f *fakeProjectsService) ListProjects(ctx context.Context, userID string) ([]Project, error) {
	var result []Project
	for _, p := range f.projects {
		if roles, ok := f.members[p.ID]; ok {
			if _, isMember := roles[userID]; isMember {
				result = append(result, p)
			}
		}
	}
	return result, nil
}

func (f *fakeProjectsService) requireAdmin(projectID, userID string) error {
	role := f.members[projectID][userID]
	if role != "owner" && role != "admin" {
		return projects.ErrProjectAdminRequired
	}
	return nil
}

func (f *fakeProjectsService) CreateToken(ctx context.Context, projectID, userID string, options TokenCreateOptions) (string, ProjectToken, error) {
	description, scopes := options.Description, options.Scopes
	if _, ok := f.projects[projectID]; !ok {
		return "", ProjectToken{}, errors.New("project not found")
	}
	if err := f.requireAdmin(projectID, userID); err != nil {
		return "", ProjectToken{}, err
	}
	f.tokCounter++
	id := "tok-" + itoa(f.tokCounter)
	plainToken := "ascope_" + id + "00randomdatafortestingpurposes"
	t := ProjectToken{
		ID:          id,
		ProjectID:   projectID,
		TokenPrefix: "ascope_" + id,
		Scopes:      scopes,
		Description: description,
	}
	f.tokens[id] = t
	return plainToken, t, nil
}

func (f *fakeProjectsService) ListTokens(ctx context.Context, projectID, userID string) ([]ProjectToken, error) {
	if err := f.requireAdmin(projectID, userID); err != nil {
		return nil, err
	}
	out := []ProjectToken{}
	for _, token := range f.tokens {
		if token.ProjectID == projectID {
			out = append(out, token)
		}
	}
	return out, nil
}
func (f *fakeProjectsService) RevokeToken(ctx context.Context, projectID, tokenID, userID, reason string) (ProjectToken, error) {
	if err := f.requireAdmin(projectID, userID); err != nil {
		return ProjectToken{}, err
	}
	token, ok := f.tokens[tokenID]
	if !ok {
		return ProjectToken{}, errors.New("not found")
	}
	token.RevokedAt = "2026-07-28T00:00:00Z"
	token.RevokedBy = userID
	token.RevocationReason = reason
	token.Status = "revoked"
	f.tokens[tokenID] = token
	return token, nil
}
func (f *fakeProjectsService) RotateToken(ctx context.Context, projectID, tokenID, userID string, options TokenCreateOptions) (string, ProjectToken, error) {
	return f.CreateToken(ctx, projectID, userID, options)
}
func (f *fakeProjectsService) ListTokenAudit(ctx context.Context, projectID, tokenID, userID string) ([]TokenAuditEvent, error) {
	if err := f.requireAdmin(projectID, userID); err != nil {
		return nil, err
	}
	return []TokenAuditEvent{}, nil
}
func (f *fakeProjectsService) GetTokenPolicy(ctx context.Context, projectID, userID string) (TokenPolicy, error) {
	if err := f.requireAdmin(projectID, userID); err != nil {
		return TokenPolicy{}, err
	}
	return TokenPolicy{ProjectID: projectID, AllowNonExpiring: true, ExpiryWarningDays: []int{14, 7, 1}, UnusedWarningDays: 60}, nil
}
func (f *fakeProjectsService) UpdateTokenPolicy(ctx context.Context, projectID, userID string, policy TokenPolicy) (TokenPolicy, error) {
	if err := f.requireAdmin(projectID, userID); err != nil {
		return TokenPolicy{}, err
	}
	policy.ProjectID = projectID
	return policy, nil
}

func itoa(i int) string {
	if i == 0 {
		return "0"
	}
	s := ""
	for i > 0 {
		s = string(rune('0'+i%10)) + s
		i /= 10
	}
	return s
}

// ---- Fake Datasets Service ----

type fakeDatasetsService struct {
	datasets map[string]Dataset
	cases    map[string][]DatasetCase
	counter  int
}

func newFakeDatasetsService() *fakeDatasetsService {
	return &fakeDatasetsService{
		datasets: make(map[string]Dataset),
		cases:    make(map[string][]DatasetCase),
	}
}

func (f *fakeDatasetsService) CreateDataset(ctx context.Context, projectID, name, description string) (Dataset, error) {
	f.counter++
	id := "ds-" + itoa(f.counter)
	ds := Dataset{
		ID:          id,
		ProjectID:   projectID,
		Name:        name,
		Description: description,
		CreatedAt:   "2026-07-15T12:00:00Z",
	}
	f.datasets[id] = ds
	return ds, nil
}

func (f *fakeDatasetsService) AddCase(ctx context.Context, datasetID string, input, expected, metadata json.RawMessage) (DatasetCase, error) {
	f.counter++
	id := "case-" + itoa(f.counter)
	dc := DatasetCase{
		ID:        id,
		DatasetID: datasetID,
		Input:     input,
		Expected:  expected,
		Metadata:  metadata,
		SortOrder: len(f.cases[datasetID]) + 1,
		CreatedAt: "2026-07-15T12:00:00Z",
	}
	f.cases[datasetID] = append(f.cases[datasetID], dc)
	return dc, nil
}

func (f *fakeDatasetsService) ListCases(ctx context.Context, datasetID string) ([]DatasetCase, error) {
	return f.cases[datasetID], nil
}

func (f *fakeDatasetsService) GetDataset(ctx context.Context, datasetID string) (Dataset, error) {
	ds, ok := f.datasets[datasetID]
	if !ok {
		return Dataset{}, errors.New("dataset not found")
	}
	return ds, nil
}

func (f *fakeDatasetsService) ListDatasets(ctx context.Context, projectID string) ([]Dataset, error) {
	var result []Dataset
	for _, ds := range f.datasets {
		if ds.ProjectID == projectID {
			result = append(result, ds)
		}
	}
	return result, nil
}

// ---- Fake Experiments Service ----

type fakeExperimentsService struct {
	experiments map[string]Experiment
	results     map[string][]EvalCaseResult
	counter     int
}

func newFakeExperimentsService() *fakeExperimentsService {
	return &fakeExperimentsService{
		experiments: make(map[string]Experiment),
		results:     make(map[string][]EvalCaseResult),
	}
}

func (f *fakeExperimentsService) CreateExperiment(ctx context.Context, projectID, datasetID, evaluatorID, targetName, targetVersion string, caseIDs []string) (Experiment, error) {
	f.counter++
	id := "exp-" + itoa(f.counter)
	e := Experiment{
		ID:          id,
		ProjectID:   projectID,
		DatasetID:   datasetID,
		EvaluatorID: evaluatorID,
		TargetName:  targetName,
		Status:      "created",
		CreatedAt:   "2026-07-15T12:00:00Z",
	}
	f.experiments[id] = e
	return e, nil
}

func (f *fakeExperimentsService) GetExperiment(ctx context.Context, experimentID string) (ExperimentDetail, error) {
	e, ok := f.experiments[experimentID]
	if !ok {
		return ExperimentDetail{}, errors.New("experiment not found")
	}
	return ExperimentDetail{Experiment: e, StatusCounts: map[string]int{"queued": 0}}, nil
}

func (f *fakeExperimentsService) ListExperiments(ctx context.Context, projectID string) ([]Experiment, error) {
	var result []Experiment
	for _, e := range f.experiments {
		if e.ProjectID == projectID {
			result = append(result, e)
		}
	}
	return result, nil
}

func (f *fakeExperimentsService) ListExperimentResults(ctx context.Context, experimentID string) ([]EvalCaseResult, error) {
	return f.results[experimentID], nil
}

func (f *fakeExperimentsService) ListExperimentJobs(ctx context.Context, experimentID string) ([]EvalJob, error) {
	return []EvalJob{}, nil
}

func (f *fakeExperimentsService) UpdateExperimentHarnessMetadata(ctx context.Context, experimentID, modelProvider, modelName, modelVersion, promptVersion, harnessVersion, datasetVersion string, featureFlags []byte) error {
	return nil // no-op in fake
}

func (f *fakeExperimentsService) UpdateFullExperimentHarnessMetadata(ctx context.Context, experimentID, modelProvider, modelName, modelVersion, promptVersion, harnessVersion, datasetVersion, toolsetVersion, skillsetVersion, ragConfigVersion string, featureFlags []byte) error {
	return nil // no-op in fake
}

func (f *fakeExperimentsService) CreateExperimentWithHarness(ctx context.Context, projectID, datasetID, evaluatorID string, params experiments.CreateExperimentHarnessParams) (Experiment, error) {
	return f.CreateExperiment(ctx, projectID, datasetID, evaluatorID, params.TargetName, params.TargetVersion, params.CaseIDs)
}

// ---- Fake Evals Service ----

type fakeEvalsService struct {
	definitions map[string]evals.MetricDefinition
	scores      map[string]evals.EvalScore
	dsCounter   int
	scCounter   int
}

func newFakeEvalsService() *fakeEvalsService {
	return &fakeEvalsService{
		definitions: make(map[string]evals.MetricDefinition),
		scores:      make(map[string]evals.EvalScore),
	}
}

func (f *fakeEvalsService) CreateMetricDefinition(ctx context.Context, projectID, name, kind, config string) (evals.MetricDefinition, error) {
	f.dsCounter++
	id := "md-" + itoa(f.dsCounter)
	d := evals.MetricDefinition{
		ID:        id,
		ProjectID: projectID,
		Name:      name,
		Kind:      kind,
		CreatedAt: time.Now(),
	}
	f.definitions[id] = d
	return d, nil
}

func (f *fakeEvalsService) DeleteMetricDefinition(ctx context.Context, projectID, definitionID string) error {
	delete(f.definitions, definitionID)
	return nil
}
func (f *fakeEvalsService) UpdateMetricDefinition(ctx context.Context, projectID, definitionID, name, kind, config string) (evals.MetricDefinition, error) {
	d, ok := f.definitions[definitionID]
	if !ok {
		return evals.MetricDefinition{}, errors.New("not found")
	}
	d.Name = name
	d.Kind = kind
	f.definitions[definitionID] = d
	return d, nil
}

func (f *fakeEvalsService) ListMetricDefinitions(ctx context.Context, projectID string) ([]evals.MetricDefinition, error) {
	var result []evals.MetricDefinition
	for _, d := range f.definitions {
		if d.ProjectID == projectID {
			result = append(result, d)
		}
	}
	return result, nil
}

func (f *fakeEvalsService) CreateEvalScore(ctx context.Context, score evals.EvalScore) (evals.EvalScore, error) {
	f.scCounter++
	id := "es-" + itoa(f.scCounter)
	score.ID = id
	score.CreatedAt = time.Now()
	f.scores[id] = score
	return score, nil
}

func (f *fakeEvalsService) CreateEvalScores(ctx context.Context, scores []evals.EvalScore) ([]evals.EvalScore, error) {
	results := make([]evals.EvalScore, 0, len(scores))
	for _, score := range scores {
		result, err := f.CreateEvalScore(ctx, score)
		if err != nil {
			return nil, err
		}
		results = append(results, result)
	}
	return results, nil
}

func (f *fakeEvalsService) ListEvalScoresByRun(ctx context.Context, projectID, runID string) ([]evals.EvalScore, error) {
	var result []evals.EvalScore
	for _, s := range f.scores {
		if s.RunID == runID && s.ProjectID == projectID {
			result = append(result, s)
		}
	}
	return result, nil
}

func (f *fakeEvalsService) ListEvalScoresByRuns(ctx context.Context, projectID string, runIDs []string) ([]evals.EvalScore, error) {
	var result []evals.EvalScore
	for _, s := range f.scores {
		for _, rid := range runIDs {
			if s.RunID == rid && s.ProjectID == projectID {
				result = append(result, s)
			}
		}
	}
	return result, nil
}

func (f *fakeEvalsService) AggregateEvalScoresByExperiment(ctx context.Context, projectID, experimentID string) ([]evals.MetricKindAggregation, error) {
	return nil, nil
}

// ---- Fake ProjectsService that enforces project membership ----

type fakeProjectAuth struct {
	members map[string]map[string]string // projectID -> userID -> role
}

func newFakeProjectAuth() *fakeProjectAuth {
	return &fakeProjectAuth{
		members: make(map[string]map[string]string),
	}
}

func (f *fakeProjectAuth) IsProjectMember(ctx context.Context, userID, projectID string) error {
	if roles, ok := f.members[projectID]; ok {
		if _, isMember := roles[userID]; isMember {
			return nil
		}
	}
	return errors.New("not a project member")
}

func (f *fakeProjectAuth) allow(userID, projectID string) {
	if f.members[projectID] == nil {
		f.members[projectID] = make(map[string]string)
	}
	f.members[projectID][userID] = "member"
}

// ---- Fake Sampling Service ----

type fakeSamplingService struct {
	rules map[string]samplingRuleEntry
	idSeq int
}

type samplingRuleEntry struct {
	rule    samplingRuleData
	deleted bool
}

type samplingRuleData struct {
	projectID  string
	agentID    string
	name       string
	enabled    bool
	sampleRate float32
	filters    string
}

func newFakeSamplingService() *fakeSamplingService {
	return &fakeSamplingService{
		rules: make(map[string]samplingRuleEntry),
	}
}

func (f *fakeSamplingService) CreateRule(ctx context.Context, params sampling.CreateRuleParams) (sampling.SamplingRule, error) {
	params, err := sampling.ValidateCreateRuleParams(params)
	if err != nil {
		return sampling.SamplingRule{}, err
	}
	f.idSeq++
	id := "sr-" + itoa(f.idSeq)
	rule := sampling.SamplingRule{
		ID:         id,
		ProjectID:  params.ProjectID,
		AgentID:    params.AgentID,
		Name:       params.Name,
		Enabled:    true,
		SampleRate: params.SampleRate,
		Filters:    params.Filters,
	}
	f.rules[id] = samplingRuleEntry{
		rule: samplingRuleData{
			projectID:  params.ProjectID,
			agentID:    params.AgentID,
			name:       params.Name,
			enabled:    true,
			sampleRate: params.SampleRate,
			filters:    params.Filters,
		},
	}
	return rule, nil
}

func (f *fakeSamplingService) ListRules(ctx context.Context, projectID string) ([]sampling.SamplingRule, error) {
	var result []sampling.SamplingRule
	for id, entry := range f.rules {
		if entry.deleted {
			continue
		}
		if entry.rule.projectID == projectID {
			result = append(result, sampling.SamplingRule{
				ID:         id,
				ProjectID:  entry.rule.projectID,
				AgentID:    entry.rule.agentID,
				Name:       entry.rule.name,
				Enabled:    entry.rule.enabled,
				SampleRate: entry.rule.sampleRate,
				Filters:    entry.rule.filters,
			})
		}
	}
	return result, nil
}

func (f *fakeSamplingService) GetRule(ctx context.Context, ruleID string) (sampling.SamplingRule, error) {
	entry, ok := f.rules[ruleID]
	if !ok || entry.deleted {
		return sampling.SamplingRule{}, errors.New("rule not found")
	}
	return sampling.SamplingRule{
		ID:         ruleID,
		ProjectID:  entry.rule.projectID,
		AgentID:    entry.rule.agentID,
		Name:       entry.rule.name,
		Enabled:    entry.rule.enabled,
		SampleRate: entry.rule.sampleRate,
		Filters:    entry.rule.filters,
	}, nil
}

func (f *fakeSamplingService) UpdateRule(ctx context.Context, projectID, ruleID string, params sampling.UpdateRuleParams) (sampling.SamplingRule, error) {
	params, err := sampling.ValidateUpdateRuleParams(params)
	if err != nil {
		return sampling.SamplingRule{}, err
	}
	entry, ok := f.rules[ruleID]
	if !ok || entry.deleted || entry.rule.projectID != projectID {
		return sampling.SamplingRule{}, sampling.ErrRuleNotFound
	}
	if params.Name != nil {
		entry.rule.name = *params.Name
	}
	if params.Enabled != nil {
		entry.rule.enabled = *params.Enabled
	}
	if params.SampleRate != nil {
		entry.rule.sampleRate = *params.SampleRate
	}
	if params.Filters != nil {
		entry.rule.filters = *params.Filters
	}
	f.rules[ruleID] = entry
	return sampling.SamplingRule{
		ID:         ruleID,
		ProjectID:  entry.rule.projectID,
		AgentID:    entry.rule.agentID,
		Name:       entry.rule.name,
		Enabled:    entry.rule.enabled,
		SampleRate: entry.rule.sampleRate,
		Filters:    entry.rule.filters,
	}, nil
}

func (f *fakeSamplingService) DeleteRule(ctx context.Context, ruleID string) error {
	entry, ok := f.rules[ruleID]
	if !ok || entry.deleted {
		return errors.New("rule not found")
	}
	entry.deleted = true
	f.rules[ruleID] = entry
	return nil
}

// ---- Fake Runner Service ----

type fakeRunnerService struct {
	claimableJobs []ClaimResponse
	claimErr      error
	submitted     []SubmitResultRequest
	failedJobs    []string
	lastSubmitReq *SubmitResultRequest
}

func newFakeRunnerService() *fakeRunnerService {
	return &fakeRunnerService{}
}

func (f *fakeRunnerService) addClaimableJob() string {
	jobID := "job-" + itoa(len(f.claimableJobs)+1)
	f.claimableJobs = append(f.claimableJobs, ClaimResponse{
		Job: EvalJob{
			ID:            jobID,
			ExperimentID:  "exp-1",
			DatasetCaseID: "case-1",
			Status:        "queued",
		},
		Status:        "queued",
		LeaseDuration: "30s",
		TargetName:    "test-agent",
		CaseInput:     `{"query":"hello"}`,
	})
	return jobID
}

func (f *fakeRunnerService) Claim(ctx context.Context, projectID, runnerName string) (ClaimResponse, error) {
	if f.claimErr != nil {
		return ClaimResponse{}, f.claimErr
	}
	if len(f.claimableJobs) == 0 {
		return ClaimResponse{}, errors.New("no jobs available")
	}
	job := f.claimableJobs[0]
	f.claimableJobs = f.claimableJobs[1:]
	return job, nil
}

func (f *fakeRunnerService) Heartbeat(ctx context.Context, jobID, projectID, runnerName string) error {
	return nil
}

func (f *fakeRunnerService) SubmitResult(ctx context.Context, jobID, projectID, runnerName string, req SubmitResultRequest) error {
	f.submitted = append(f.submitted, req)
	f.lastSubmitReq = &req
	return nil
}

func (f *fakeRunnerService) FailJob(ctx context.Context, jobID, projectID, runnerName, errorMsg string) error {
	f.failedJobs = append(f.failedJobs, jobID)
	return nil
}

// ---- Fake Annotation Service ----

type fakeAnnotationService struct {
	queues map[string]AnnotationQueue
	items  map[string]AnnotationItem
	qSeq   int
	iSeq   int
	dsSeq  int
}

func newFakeAnnotationService() *fakeAnnotationService {
	return &fakeAnnotationService{
		queues: make(map[string]AnnotationQueue),
		items:  make(map[string]AnnotationItem),
	}
}

func (f *fakeAnnotationService) CreateQueue(ctx context.Context, projectID, name, description string) (AnnotationQueue, error) {
	f.qSeq++
	id := "queue-" + itoa(f.qSeq)
	q := AnnotationQueue{
		ID:          id,
		ProjectID:   projectID,
		Name:        name,
		Description: description,
	}
	f.queues[id] = q
	return q, nil
}

func (f *fakeAnnotationService) ListQueues(ctx context.Context, projectID string) ([]AnnotationQueue, error) {
	var result []AnnotationQueue
	for _, q := range f.queues {
		if q.ProjectID == projectID {
			result = append(result, q)
		}
	}
	return result, nil
}

func (f *fakeAnnotationService) GetQueue(ctx context.Context, queueID string) (AnnotationQueue, error) {
	q, ok := f.queues[queueID]
	if !ok {
		return AnnotationQueue{}, errors.New("queue not found")
	}
	return q, nil
}

func (f *fakeAnnotationService) CreateItem(ctx context.Context, queueID, runID, traceID string, priority int, scores string) (AnnotationItem, error) {
	f.iSeq++
	id := "item-" + itoa(f.iSeq)
	q, ok := f.queues[queueID]
	if !ok {
		return AnnotationItem{}, errors.New("queue not found")
	}
	item := AnnotationItem{
		ID:        id,
		QueueID:   queueID,
		ProjectID: q.ProjectID,
		RunID:     runID,
		TraceID:   traceID,
		Status:    "pending",
		Priority:  priority,
		Scores:    scores,
	}
	f.items[id] = item
	return item, nil
}

func (f *fakeAnnotationService) CreateItemFromRun(ctx context.Context, projectID, runID, queueID string) (AnnotationItem, error) {
	f.iSeq++
	id := "item-" + itoa(f.iSeq)

	// Use specified queue or find/create default
	var q AnnotationQueue
	if queueID != "" {
		var ok bool
		q, ok = f.queues[queueID]
		if !ok {
			return AnnotationItem{}, errors.New("queue not found")
		}
	} else {
		// Find or create default queue named "线上评测待标注"
		for _, qq := range f.queues {
			if qq.ProjectID == projectID && qq.Name == "线上评测待标注" {
				q = qq
				break
			}
		}
		if q.ID == "" {
			f.qSeq++
			q = AnnotationQueue{
				ID:        "queue-default-" + itoa(f.qSeq),
				ProjectID: projectID,
				Name:      "线上评测待标注",
			}
			f.queues[q.ID] = q
		}
	}

	item := AnnotationItem{
		ID:        id,
		QueueID:   q.ID,
		ProjectID: projectID,
		RunID:     runID,
		TraceID:   "trace-" + runID,
		Status:    "pending",
		AgentName: "fake-agent",
	}
	f.items[id] = item
	return item, nil
}

func (f *fakeAnnotationService) ListItemsByQueue(ctx context.Context, queueID string) ([]AnnotationItem, error) {
	var result []AnnotationItem
	for _, item := range f.items {
		if item.QueueID == queueID {
			result = append(result, item)
		}
	}
	return result, nil
}

func (f *fakeAnnotationService) ListItemsByProject(ctx context.Context, projectID string) ([]AnnotationItem, error) {
	var result []AnnotationItem
	for _, item := range f.items {
		if item.ProjectID == projectID {
			result = append(result, item)
		}
	}
	return result, nil
}

func (f *fakeAnnotationService) GetItem(ctx context.Context, itemID string) (AnnotationItem, error) {
	item, ok := f.items[itemID]
	if !ok {
		return AnnotationItem{}, errors.New("item not found")
	}
	return item, nil
}

func (f *fakeAnnotationService) UpdateItem(ctx context.Context, itemID string, status, correctedOutput, expectedToolPath, comment string) (AnnotationItem, error) {
	item, ok := f.items[itemID]
	if !ok {
		return AnnotationItem{}, errors.New("item not found")
	}
	item.Status = status
	item.CorrectedOutput = correctedOutput
	item.ExpectedToolPath = expectedToolPath
	item.Comment = comment
	f.items[itemID] = item
	return item, nil
}

func (f *fakeAnnotationService) UpdateItemFull(ctx context.Context, itemID string, status, correctedOutput, expectedToolPath, comment, failureCategory string, privacyReviewed bool, reviewerNote string) (AnnotationItem, error) {
	item, ok := f.items[itemID]
	if !ok {
		return AnnotationItem{}, errors.New("item not found")
	}
	item.Status = status
	item.CorrectedOutput = correctedOutput
	item.ExpectedToolPath = expectedToolPath
	item.Comment = comment
	item.FailureCategory = failureCategory
	item.PrivacyReviewed = privacyReviewed
	item.ReviewerNote = reviewerNote
	f.items[itemID] = item
	return item, nil
}

func (f *fakeAnnotationService) UpdateItemStatus(ctx context.Context, itemID, status string) (AnnotationItem, error) {
	item, ok := f.items[itemID]
	if !ok {
		return AnnotationItem{}, errors.New("item not found")
	}
	item.Status = status
	f.items[itemID] = item
	return item, nil
}

func (f *fakeAnnotationService) PromoteToDataset(ctx context.Context, itemID, datasetID string) (string, error) {
	item, ok := f.items[itemID]
	if !ok {
		return "", errors.New("item not found")
	}
	if item.Status == "promoted" {
		return "", errors.New("annotation item already promoted")
	}
	if item.CorrectedOutput == "" {
		return "", errors.New("annotation item has no corrected output")
	}
	f.dsSeq++
	item.Status = "promoted"
	f.items[itemID] = item
	return "case-" + itoa(f.dsSeq), nil
}

func (f *fakeAnnotationService) SubmitGold(
	ctx context.Context,
	itemID, projectID, goldScores, annotatorID string,
) (AnnotationItem, error) {
	item, ok := f.items[itemID]
	if !ok || item.ProjectID != projectID {
		return AnnotationItem{}, errors.New("item not found")
	}
	item.GoldScores = goldScores
	item.IsGold = true
	item.AnnotatorID = annotatorID
	item.Status = "completed"
	f.items[itemID] = item
	return item, nil
}

func (f *fakeProjectsService) GetTokenRiskSummary(ctx context.Context, projectID, userID string) (TokenRiskSummary, error) {
	if err := f.requireAdmin(projectID, userID); err != nil {
		return TokenRiskSummary{}, err
	}
	return TokenRiskSummary{}, nil
}

// ---- Fake server-authoritative sessions ----

type fakeUserSessions struct {
	signer   auth.SessionSigner
	sessions map[string]auth.Session
	counter  int
}

func newFakeUserSessions(signer auth.SessionSigner) *fakeUserSessions {
	return &fakeUserSessions{signer: signer, sessions: make(map[string]auth.Session)}
}

func (f *fakeUserSessions) Create(_ context.Context, userID, userAgent, ipPrefix string) (string, auth.Session, error) {
	f.counter++
	now := time.Now().UTC()
	session := auth.Session{
		ID:               "session-" + itoa(f.counter),
		UserID:           userID,
		CreatedAt:        now,
		ExpiresAt:        now.Add(f.signer.TTL()),
		UserAgentSummary: userAgent,
		IPPrefix:         ipPrefix,
	}
	f.sessions[session.ID] = session
	cookie, err := f.signer.SignSessionPayload(session.ID + ".test-secret-" + itoa(f.counter))
	return cookie, session, err
}

func (f *fakeUserSessions) Authenticate(_ context.Context, cookie string) (auth.Session, error) {
	payload, err := f.signer.VerifySessionPayload(cookie)
	if err != nil {
		return auth.Session{}, err
	}
	parts := strings.Split(payload, ".")
	if len(parts) != 2 {
		return auth.Session{}, auth.ErrMalformedSession
	}
	session, ok := f.sessions[parts[0]]
	if !ok || session.RevokedAt != nil || !session.ExpiresAt.After(time.Now()) {
		return auth.Session{}, auth.ErrSessionNotFound
	}
	return session, nil
}

func (f *fakeUserSessions) RevokeCurrent(_ context.Context, sessionID, userID, reason string) error {
	session, ok := f.sessions[sessionID]
	if !ok || session.UserID != userID || session.RevokedAt != nil {
		return auth.ErrSessionNotFound
	}
	now := time.Now().UTC()
	session.RevokedAt = &now
	session.RevokedReason = reason
	f.sessions[sessionID] = session
	return nil
}

func (f *fakeUserSessions) RevokeAll(_ context.Context, userID, exceptSessionID, reason string) error {
	now := time.Now().UTC()
	for id, session := range f.sessions {
		if session.UserID != userID || id == exceptSessionID || session.RevokedAt != nil {
			continue
		}
		session.RevokedAt = &now
		session.RevokedReason = reason
		f.sessions[id] = session
	}
	return nil
}

func (f *fakeUserSessions) List(_ context.Context, userID string) ([]auth.Session, error) {
	out := make([]auth.Session, 0)
	for _, session := range f.sessions {
		if session.UserID == userID {
			out = append(out, session)
		}
	}
	return out, nil
}
