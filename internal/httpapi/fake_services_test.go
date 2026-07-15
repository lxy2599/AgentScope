package httpapi

import (
	"context"
	"encoding/json"
	"errors"
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
		return "", errors.New("email already exists")
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
		return "", "", errors.New("invalid credentials")
	}
	if f.passwords[userID] != password+":hashed" {
		return "", "", errors.New("invalid credentials")
	}
	return userID, f.displayNames[userID], nil
}

// ---- Fake Projects Service ----

type fakeProjectsService struct {
	projects    map[string]Project // projectID -> project
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

func (f *fakeProjectsService) CreateToken(ctx context.Context, projectID, userID, description string, scopes []string) (string, ProjectToken, error) {
	if _, ok := f.projects[projectID]; !ok {
		return "", ProjectToken{}, errors.New("project not found")
	}
	if roles, ok := f.members[projectID]; ok {
		if _, member := roles[userID]; !member {
			return "", ProjectToken{}, errors.New("not a project member")
		}
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

// ---- Fake Experiments Service ----

type fakeExperimentsService struct {
	experiments map[string]Experiment
	details     map[string]ExperimentDetail
	counter     int
}

func newFakeExperimentsService() *fakeExperimentsService {
	return &fakeExperimentsService{
		experiments: make(map[string]Experiment),
		details:     make(map[string]ExperimentDetail),
	}
}

func (f *fakeExperimentsService) CreateExperiment(ctx context.Context, projectID, datasetID, evaluatorID, targetName, targetVersion string, caseIDs []string) (Experiment, error) {
	f.counter++
	id := "exp-" + itoa(f.counter)
	exp := Experiment{
		ID:            id,
		ProjectID:     projectID,
		DatasetID:     datasetID,
		EvaluatorID:   evaluatorID,
		TargetName:    targetName,
		TargetVersion: targetVersion,
		Status:        "pending",
		TotalCases:    len(caseIDs),
		PassedCases:   0,
		FailedCases:   0,
		CreatedAt:     "2026-07-15T12:00:00Z",
	}
	f.experiments[id] = exp
	return exp, nil
}

func (f *fakeExperimentsService) GetExperiment(ctx context.Context, experimentID string) (ExperimentDetail, error) {
	detail, ok := f.details[experimentID]
	if !ok {
		// Try creating from base experiment
		exp, ok := f.experiments[experimentID]
		if !ok {
			return ExperimentDetail{}, errors.New("experiment not found")
		}
		detail = ExperimentDetail{
			Experiment: exp,
			StatusCounts: map[string]int{
				"queued":    exp.TotalCases,
				"running":   0,
				"succeeded": 0,
				"failed":    0,
			},
		}
		f.details[experimentID] = detail
	}
	return detail, nil
}

// ---- Fake Runner Service ----

type fakeRunnerService struct {
	jobs     map[string]bool
	counter  int
}

func newFakeRunnerService() *fakeRunnerService {
	return &fakeRunnerService{
		jobs: make(map[string]bool),
	}
}

func (f *fakeRunnerService) addClaimableJob() string {
	f.counter++
	id := "job-" + itoa(f.counter)
	f.jobs[id] = true // claimable
	return id
}

func (f *fakeRunnerService) Claim(ctx context.Context, projectID, runnerName string) (ClaimedJob, error) {
	for id, claimable := range f.jobs {
		if claimable {
			f.jobs[id] = false // mark as claimed
			return ClaimedJob{
				JobID:         id,
				Status:        "running",
				LeaseDuration: "2m0s",
			}, nil
		}
	}
	return ClaimedJob{}, errors.New("no claimable jobs")
}

func (f *fakeRunnerService) Heartbeat(ctx context.Context, jobID, runnerName string) error {
	if _, ok := f.jobs[jobID]; !ok {
		return errors.New("job not found")
	}
	return nil
}

func (f *fakeRunnerService) SubmitResult(ctx context.Context, jobID, runnerName string, req SubmitResultRequest) error {
	if _, ok := f.jobs[jobID]; !ok {
		return errors.New("job not found")
	}
	return nil
}

func (f *fakeRunnerService) FailJob(ctx context.Context, jobID, runnerName, errorMsg string) error {
	if _, ok := f.jobs[jobID]; !ok {
		return errors.New("job not found")
	}
	return nil
}
