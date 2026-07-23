package runs

import (
	"context"
	"errors"
	"time"
)

type fakeRuntimeQuerier struct {
	runs             map[string]Run
	runMetadata      map[string][]byte
	events           map[string][]RunStateEvent
	eventInstanceIDs map[string][]string // runID -> []agentInstanceDBID
	activeRunIDMap   map[string]string
	knownInstances   map[string]string // instanceID -> DB UUID
	nextID           int
}

func newFakeRuntimeQuerier() *fakeRuntimeQuerier {
	return &fakeRuntimeQuerier{
		runs:           make(map[string]Run),
		runMetadata:    make(map[string][]byte),
		events:         make(map[string][]RunStateEvent),
		eventInstanceIDs: make(map[string][]string),
		activeRunIDMap: make(map[string]string),
		knownInstances: make(map[string]string),
	}
}

func newFakeRuntimeQuerierWithInstances(instances map[string]string) *fakeRuntimeQuerier {
	f := newFakeRuntimeQuerier()
	for k, v := range instances {
		f.knownInstances[k] = v
	}
	return f
}

func (f *fakeRuntimeQuerier) CreateRun(ctx context.Context, projectID, source, status, traceID, agentName, agentVersion string, metadata []byte) (Run, error) {
	f.nextID++
	id := "run-" + itoa(f.nextID)
	r := Run{
		ID:        id,
		ProjectID: projectID,
		Source:    source,
		Status:    status,
		TraceID:   traceID,
		AgentName: agentName,
		CreatedAt: time.Now(),
		UpdatedAt: time.Now(),
	}
	f.runs[id] = r
	return r, nil
}

func (f *fakeRuntimeQuerier) StartRuntimeRun(ctx context.Context, projectID, status, traceID, agentName, inputSummary string, metadata []byte) (Run, error) {
	f.nextID++
	id := "run-" + itoa(f.nextID)
	r := Run{
		ID:           id,
		ProjectID:    projectID,
		Status:       status,
		TraceID:      traceID,
		AgentName:    agentName,
		InputSummary: inputSummary,
		CreatedAt:    time.Now(),
		UpdatedAt:    time.Now(),
	}
	f.runs[id] = r
	if len(metadata) > 2 { // more than "{}"
		f.runMetadata[id] = metadata
	}
	return r, nil
}

// runMetadata stores metadata bytes keyed by run ID.
// Exists only for test verification.
type fakeRunMetadata map[string][]byte

func (f *fakeRuntimeQuerier) getRunMetadata(runID string) []byte {
	return f.runMetadata[runID]
}

func (f *fakeRuntimeQuerier) CompleteRuntimeRun(ctx context.Context, runID, projectID, outputSummary string, latencyMs int64) (Run, error) {
	r, ok := f.runs[runID]
	if !ok || r.ProjectID != projectID {
		return Run{}, errors.New("run not found")
	}
	r.Status = string(RunStatusCompleted)
	r.OutputSummary = outputSummary
	r.LatencyMs = &latencyMs
	r.UpdatedAt = time.Now()
	f.runs[runID] = r
	return r, nil
}

func (f *fakeRuntimeQuerier) FailRuntimeRun(ctx context.Context, runID, projectID, errorStr string, latencyMs int64) (Run, error) {
	r, ok := f.runs[runID]
	if !ok || r.ProjectID != projectID {
		return Run{}, errors.New("run not found")
	}
	r.Status = string(RunStatusFailed)
	r.Error = errorStr
	var ms int64 = latencyMs
	r.LatencyMs = &ms
	r.UpdatedAt = time.Now()
	f.runs[runID] = r
	return r, nil
}

func (f *fakeRuntimeQuerier) GetRunByID(ctx context.Context, runID string) (Run, error) {
	r, ok := f.runs[runID]
	if !ok {
		return Run{}, errors.New("run not found")
	}
	return r, nil
}

func (f *fakeRuntimeQuerier) UpdateRunStatus(ctx context.Context, runID, projectID, status, errorStr string) (Run, error) {
	r, ok := f.runs[runID]
	if !ok || r.ProjectID != projectID {
		return Run{}, errors.New("run not found")
	}
	r.Status = status
	r.Error = errorStr
	r.UpdatedAt = time.Now()
	f.runs[runID] = r
	return r, nil
}

func (f *fakeRuntimeQuerier) CreateRunStateEvent(ctx context.Context, projectID, runID string, agentID, agentInstanceDBID *string, fromStatus, toStatus, message string, attributes []byte) (RunStateEvent, error) {
	f.nextID++
	evt := RunStateEvent{
		ID:         "evt-" + itoa(f.nextID),
		RunID:      runID,
		FromStatus: fromStatus,
		ToStatus:   toStatus,
		Message:    message,
		CreatedAt:  time.Now(),
	}
	f.events[runID] = append(f.events[runID], evt)
	if agentInstanceDBID != nil {
		f.eventInstanceIDs[runID] = append(f.eventInstanceIDs[runID], *agentInstanceDBID)
	}
	return evt, nil
}

func (f *fakeRuntimeQuerier) ListRunStateEvents(ctx context.Context, runID string) ([]RunStateEvent, error) {
	return f.events[runID], nil
}

func (f *fakeRuntimeQuerier) ListActiveRunsByProject(ctx context.Context, projectID string, limit, offset int) ([]ActiveRun, error) {
	var result []ActiveRun
	for _, r := range f.runs {
		if r.ProjectID == projectID && !IsTerminalRunStatus(r.Status) {
			result = append(result, ActiveRun{
				ID:        r.ID,
				ProjectID: r.ProjectID,
				Status:    r.Status,
				CreatedAt: r.CreatedAt,
				UpdatedAt: r.UpdatedAt,
			})
		}
	}
	return result, nil
}

func (f *fakeRuntimeQuerier) ListRecentRunsByAgent(ctx context.Context, projectID, agentName string, limit int) ([]RecentRunEntry, error) {
	return nil, nil
}

func (f *fakeRuntimeQuerier) GetRunRuntimeDetail(ctx context.Context, runID, projectID string) (*RuntimeRunDetail, error) {
	r, ok := f.runs[runID]
	if !ok || r.ProjectID != projectID {
		return nil, errors.New("run not found")
	}
	return &RuntimeRunDetail{
		RuntimeRun: RuntimeRun{
			ID:        r.ID,
			ProjectID: r.ProjectID,
			Status:    r.Status,
			CreatedAt: r.CreatedAt,
			UpdatedAt: r.UpdatedAt,
		},
	}, nil
}

func (f *fakeRuntimeQuerier) SetAgentInstanceActiveRun(ctx context.Context, projectID, agentID, instanceID, runID string) (string, error) {
	dbID := "db-inst-uuid-" + instanceID
	f.activeRunIDMap[instanceID] = runID
	return dbID, nil
}

func (f *fakeRuntimeQuerier) ClearAgentInstanceActiveRun(ctx context.Context, projectID, agentID, instanceID string) (string, error) {
	dbID := "db-inst-uuid-" + instanceID
	delete(f.activeRunIDMap, instanceID)
	return dbID, nil
}

func (f *fakeRuntimeQuerier) GetAgentInstanceDBID(ctx context.Context, projectID, agentID, instanceID string) (string, error) {
	dbID, ok := f.knownInstances[instanceID]
	if !ok {
		return "", errors.New("instance not found")
	}
	return dbID, nil
}

func (f *fakeRuntimeQuerier) GetAgentNameByID(ctx context.Context, agentID string) (string, error) {
	// For test, return a mock name based on agent ID
	if agentID == "agent-1" || agentID == "agent-2" {
		return "test-agent", nil
	}
	return "", errors.New("agent not found")
}

