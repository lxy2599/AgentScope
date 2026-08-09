package benchlauncher

import (
	"context"
	"errors"
	"fmt"
	"net/url"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"sort"
	"strings"
	"sync"
	"syscall"
	"time"
	"unicode"
)

var ErrUnavailable = errors.New("local benchmark runner is unavailable")

type Config struct {
	Enabled       bool
	Executable    string
	WorkDir       string
	HarborWorkDir string
	ServerURL     string
	ArkAPIKey     string
	ArkBaseURL    string
	Model         string
	TokenTTL      time.Duration
}

type TokenIssuer interface {
	CreateRunnerToken(ctx context.Context, projectID, userID string, expiresAt time.Time) (plainToken, tokenID string, err error)
	RevokeRunnerToken(ctx context.Context, projectID, tokenID, userID, reason string) error
}

type Command struct {
	Executable string
	Args       []string
	Dir        string
	Env        []string
}

type Process interface {
	PID() int
	Wait() error
}

type ProcessDiagnostics interface {
	Diagnostics() string
}

type ProcessStarter interface {
	Start(ctx context.Context, command Command) (Process, error)
}

type Prerequisites interface {
	LookPath(file string) (string, error)
	WorkDirExists(path string) bool
	DockerAvailable(ctx context.Context) error
}

type Status struct {
	ProjectID      string    `json:"project_id,omitempty"`
	ExperimentID   string    `json:"experiment_id,omitempty"`
	ExecutionKind  string    `json:"execution_kind"`
	Available      bool      `json:"available"`
	State          string    `json:"state"`
	Reason         string    `json:"reason,omitempty"`
	Blockers       []string  `json:"blockers,omitempty"`
	RunnerName     string    `json:"runner_name,omitempty"`
	PID            int       `json:"pid,omitempty"`
	StartedAt      time.Time `json:"started_at,omitempty"`
	AlreadyRunning bool      `json:"already_running,omitempty"`
}

type LaunchRequest struct {
	ProjectID     string
	UserID        string
	ExperimentID  string
	ExecutionKind string
}

type Launcher struct {
	config        Config
	tokens        TokenIssuer
	starter       ProcessStarter
	prerequisites Prerequisites
	now           func() time.Time

	mu       sync.Mutex
	runners  map[string]*managedRunner
	starting map[string]Status
	last     map[string]Status
}

type managedRunner struct {
	status  Status
	tokenID string
	userID  string
	cancel  context.CancelFunc
	process Process
	done    chan struct{}
}

type launcherOptions struct {
	starter       ProcessStarter
	prerequisites Prerequisites
	now           func() time.Time
}

type Option func(*launcherOptions)

func WithProcessStarter(starter ProcessStarter) Option {
	return func(options *launcherOptions) {
		options.starter = starter
	}
}

func WithPrerequisites(prerequisites Prerequisites) Option {
	return func(options *launcherOptions) {
		options.prerequisites = prerequisites
	}
}

func WithClock(now func() time.Time) Option {
	return func(options *launcherOptions) {
		options.now = now
	}
}

func New(config Config, tokens TokenIssuer, options ...Option) *Launcher {
	configured := launcherOptions{
		starter:       execProcessStarter{},
		prerequisites: osPrerequisites{},
		now:           time.Now,
	}
	for _, option := range options {
		option(&configured)
	}
	if config.Executable == "" {
		config.Executable = "uv"
	}
	if config.WorkDir == "" {
		config.WorkDir = "python"
	}
	if absoluteWorkDir, err := filepath.Abs(config.WorkDir); err == nil {
		config.WorkDir = absoluteWorkDir
	}
	if config.HarborWorkDir == "" {
		config.HarborWorkDir = ".external/harbor"
	}
	if absoluteHarborWorkDir, err := filepath.Abs(config.HarborWorkDir); err == nil {
		config.HarborWorkDir = absoluteHarborWorkDir
	}
	if config.ServerURL == "" {
		config.ServerURL = "http://127.0.0.1:8080"
	}
	if config.ArkBaseURL == "" {
		config.ArkBaseURL = "https://ark.cn-beijing.volces.com/api/coding/v3"
	}
	if config.Model == "" {
		config.Model = "ark-code-latest"
	}
	if config.TokenTTL <= 0 {
		config.TokenTTL = 2 * time.Hour
	}
	return &Launcher{
		config:        config,
		tokens:        tokens,
		starter:       configured.starter,
		prerequisites: configured.prerequisites,
		now:           configured.now,
		runners:       make(map[string]*managedRunner),
		starting:      make(map[string]Status),
		last:          make(map[string]Status),
	}
}

func (l *Launcher) Availability(ctx context.Context, projectID, executionKind, experimentID string) Status {
	status := Status{
		ProjectID:     projectID,
		ExperimentID:  experimentID,
		ExecutionKind: executionKind,
		Available:     true,
		State:         "idle",
	}
	if !l.config.Enabled {
		status.Blockers = append(status.Blockers, "Local benchmark launching is disabled. Set AGENTSCOPE_BENCHMARK_RUNNER_ENABLED=true on the AgentScope server.")
	}
	if !validExecutionKind(executionKind) {
		status.Blockers = append(status.Blockers, "Benchmark execution kind is invalid.")
	}
	if strings.TrimSpace(l.config.ArkAPIKey) == "" {
		status.Blockers = append(status.Blockers, "ARK_API_KEY is not configured on the AgentScope server.")
	}
	if parsed, err := url.Parse(l.config.ServerURL); err != nil || (parsed.Scheme != "http" && parsed.Scheme != "https") || parsed.Host == "" {
		status.Blockers = append(status.Blockers, "AGENTSCOPE_BENCHMARK_RUNNER_SERVER_URL must be an absolute HTTP URL.")
	}
	if !l.prerequisites.WorkDirExists(l.config.WorkDir) {
		status.Blockers = append(status.Blockers, "AGENTSCOPE_BENCHMARK_RUNNER_WORKDIR does not exist or is not a directory.")
	}
	if executionKind == "harbor" && !l.prerequisites.WorkDirExists(l.config.HarborWorkDir) {
		status.Blockers = append(status.Blockers, "AGENTSCOPE_BENCHMARK_HARBOR_WORKDIR does not exist or is not a directory.")
	}
	if _, err := l.prerequisites.LookPath(l.config.Executable); err != nil {
		status.Blockers = append(status.Blockers, "AGENTSCOPE_BENCHMARK_RUNNER_EXECUTABLE is not available on the AgentScope server.")
	}
	if executionKind == "harbor" {
		dockerCtx, cancel := context.WithTimeout(ctx, 3*time.Second)
		err := l.prerequisites.DockerAvailable(dockerCtx)
		cancel()
		if err != nil {
			status.Blockers = append(status.Blockers, "Docker must be installed and reachable for Harbor benchmark execution.")
		}
	}
	if len(status.Blockers) > 0 {
		status.Available = false
		status.State = "blocked"
		status.Reason = status.Blockers[0]
	}

	l.mu.Lock()
	defer l.mu.Unlock()
	if managed, ok := l.runners[runnerKey(projectID, executionKind, experimentID)]; ok {
		return managed.status
	}
	if starting, ok := l.starting[runnerKey(projectID, executionKind, experimentID)]; ok {
		return starting
	}
	if last, ok := l.last[runnerKey(projectID, executionKind, experimentID)]; ok && status.Available {
		status.State = last.State
		status.Reason = last.Reason
	}
	return status
}

func (l *Launcher) Launch(ctx context.Context, request LaunchRequest) (Status, error) {
	if request.ProjectID == "" || request.UserID == "" || request.ExecutionKind == "" {
		return Status{ProjectID: request.ProjectID, ExecutionKind: request.ExecutionKind, Available: false, State: "blocked", Reason: "Project, user, and execution kind are required."}, ErrUnavailable
	}
	status := l.Availability(ctx, request.ProjectID, request.ExecutionKind, request.ExperimentID)
	if !status.Available {
		return status, ErrUnavailable
	}
	key := runnerKey(request.ProjectID, request.ExecutionKind, request.ExperimentID)
	l.mu.Lock()
	if existing, ok := l.runners[key]; ok {
		status := existing.status
		status.AlreadyRunning = true
		l.mu.Unlock()
		return status, nil
	}
	if starting, ok := l.starting[key]; ok {
		starting.AlreadyRunning = true
		l.mu.Unlock()
		return starting, nil
	}
	l.starting[key] = Status{
		ProjectID:     request.ProjectID,
		ExperimentID:  request.ExperimentID,
		ExecutionKind: request.ExecutionKind,
		Available:     true,
		State:         "starting",
		RunnerName:    runnerName(request.ProjectID, request.ExecutionKind, request.ExperimentID),
	}
	l.mu.Unlock()
	defer func() {
		l.mu.Lock()
		delete(l.starting, key)
		l.mu.Unlock()
	}()

	if l.tokens == nil {
		status.Available = false
		status.State = "blocked"
		status.Reason = "Project token service is unavailable."
		status.Blockers = []string{status.Reason}
		return status, ErrUnavailable
	}

	expiresAt := l.now().UTC().Add(l.config.TokenTTL)
	plainToken, tokenID, err := l.tokens.CreateRunnerToken(ctx, request.ProjectID, request.UserID, expiresAt)
	if err != nil {
		return status, fmt.Errorf("create benchmark runner token: %w", err)
	}

	processCtx, cancel := context.WithCancel(context.Background())
	command := l.command(request, plainToken)
	process, err := l.starter.Start(processCtx, command)
	if err != nil {
		cancel()
		_ = l.tokens.RevokeRunnerToken(context.Background(), request.ProjectID, tokenID, request.UserID, "benchmark runner failed to start")
		return status, fmt.Errorf("start local benchmark runner: %w", err)
	}

	started := l.now().UTC()
	managed := &managedRunner{
		status: Status{
			ProjectID:     request.ProjectID,
			ExecutionKind: request.ExecutionKind,
			Available:     true,
			State:         "running",
			RunnerName:    runnerName(request.ProjectID, request.ExecutionKind, request.ExperimentID),
			PID:           process.PID(),
			StartedAt:     started,
		},
		tokenID: tokenID,
		userID:  request.UserID,
		cancel:  cancel,
		process: process,
		done:    make(chan struct{}),
	}

	l.mu.Lock()
	l.runners[key] = managed
	l.mu.Unlock()

	go l.watch(key, managed)
	return managed.status, nil
}

func (l *Launcher) Close(ctx context.Context) error {
	l.mu.Lock()
	runners := make([]*managedRunner, 0, len(l.runners))
	for _, managed := range l.runners {
		runners = append(runners, managed)
	}
	l.mu.Unlock()
	for _, managed := range runners {
		managed.cancel()
	}

	done := make(chan struct{})
	go func() {
		for _, managed := range runners {
			<-managed.done
		}
		close(done)
	}()
	select {
	case <-done:
		return nil
	case <-ctx.Done():
		return ctx.Err()
	}
}

func (l *Launcher) watch(key string, managed *managedRunner) {
	err := managed.process.Wait()
	defer close(managed.done)
	managed.cancel()
	_ = l.tokens.RevokeRunnerToken(context.Background(), managed.status.ProjectID, managed.tokenID, managed.userID, "benchmark runner stopped")

	stopped := managed.status
	stopped.State = "stopped"
	stopped.PID = 0
	if err != nil {
		stopped.Reason = unexpectedRunnerExitReason(managed.process)
	} else {
		stopped.Reason = "The local benchmark runner stopped."
	}
	l.mu.Lock()
	if current, ok := l.runners[key]; ok && current == managed {
		delete(l.runners, key)
		l.last[key] = stopped
	}
	l.mu.Unlock()
}

func (l *Launcher) command(request LaunchRequest, projectToken string) Command {
	return Command{
		Executable: l.config.Executable,
		Args: []string{
			"run",
			"--directory", l.config.WorkDir,
			"--frozen",
			"agentscope-runner",
			"start",
			"--execution-kind", request.ExecutionKind,
			"--exit-when-idle",
		},
		Dir: filepath.Dir(l.config.WorkDir),
		Env: runnerEnvironment(os.Environ(), map[string]string{
			"AGENTSCOPE_PROJECT_TOKEN":           projectToken,
			"AGENTSCOPE_SERVER_URL":              l.config.ServerURL,
			"AGENTSCOPE_RUNNER_NAME":             runnerName(request.ProjectID, request.ExecutionKind, request.ExperimentID),
			"AGENTSCOPE_BENCHMARK_EXPERIMENT_ID": request.ExperimentID,
			"OPENAI_API_KEY":                     l.config.ArkAPIKey,
			"OPENAI_BASE_URL":                    l.config.ArkBaseURL,
			"LLM_API_KEY":                        l.config.ArkAPIKey,
			"LLM_BASE_URL":                       l.config.ArkBaseURL,
			"AGENTSCOPE_BENCHMARK_MODEL":         l.config.Model,
			"AGENTSCOPE_HARBOR_WORKDIR":          l.config.HarborWorkDir,
			"OPENHANDS_AGENTSCOPE_ENABLED":       "true",
			"OPENHANDS_AGENTSCOPE_CAPTURE_LEVEL": "L0",
		}),
	}
}

func runnerEnvironment(parent []string, values map[string]string) []string {
	blocked := map[string]struct{}{
		"ARK_API_KEY": {}, "OPENAI_API_KEY": {}, "OPENAI_BASE_URL": {}, "LLM_API_KEY": {}, "LLM_BASE_URL": {},
		"AGENTSCOPE_PROJECT_TOKEN": {}, "AGENTSCOPE_SERVER_URL": {}, "AGENTSCOPE_RUNNER_NAME": {},
		"AGENTSCOPE_BENCHMARK_MODEL": {}, "AGENTSCOPE_BENCHMARK_EXPERIMENT_ID": {}, "AGENTSCOPE_HARBOR_WORKDIR": {}, "OPENHANDS_AGENTSCOPE_ENABLED": {},
		"OPENHANDS_AGENTSCOPE_CAPTURE_LEVEL": {},
	}
	env := make([]string, 0, len(parent)+len(values))
	for _, entry := range parent {
		key, _, found := strings.Cut(entry, "=")
		if !found {
			continue
		}
		if _, skip := blocked[key]; !skip {
			env = append(env, entry)
		}
	}
	keys := make([]string, 0, len(values))
	for key := range values {
		keys = append(keys, key)
	}
	sort.Strings(keys)
	for _, key := range keys {
		env = append(env, key+"="+values[key])
	}
	return env
}

func runnerKey(projectID, executionKind, experimentID string) string {
	return projectID + "\x00" + executionKind + "\x00" + experimentID
}

func runnerName(projectID, executionKind, experimentID string) string {
	return "benchmark-" + sanitizeName(projectID) + "-" + executionKind + "-" + sanitizeName(experimentID)
}

func sanitizeName(value string) string {
	var builder strings.Builder
	for _, r := range value {
		if (r >= 'a' && r <= 'z') || (r >= 'A' && r <= 'Z') || (r >= '0' && r <= '9') || r == '-' || r == '_' {
			builder.WriteRune(r)
		} else {
			builder.WriteByte('-')
		}
	}
	return builder.String()
}

func validExecutionKind(value string) bool {
	if value == "" || len(value) > 64 {
		return false
	}
	for index, r := range value {
		if (r >= 'a' && r <= 'z') || (r >= '0' && r <= '9') || (index > 0 && (r == '-' || r == '_')) {
			continue
		}
		return false
	}
	return true
}

type execProcessStarter struct{}

func (execProcessStarter) Start(ctx context.Context, command Command) (Process, error) {
	cmd := exec.Command(command.Executable, command.Args...)
	cmd.Dir = command.Dir
	cmd.Env = command.Env
	cmd.SysProcAttr = &syscall.SysProcAttr{Setpgid: true}
	diagnostics := newBoundedDiagnostics(4096)
	cmd.Stdout = diagnostics
	cmd.Stderr = diagnostics
	if err := cmd.Start(); err != nil {
		return nil, err
	}
	go terminateProcessGroupOnCancel(ctx, cmd.Process.Pid)
	return &execProcess{cmd: cmd, diagnostics: diagnostics}, nil
}

func terminateProcessGroupOnCancel(ctx context.Context, processID int) {
	<-ctx.Done()
	if processID > 0 {
		_ = syscall.Kill(-processID, syscall.SIGTERM)
	}
}

type execProcess struct {
	cmd         *exec.Cmd
	diagnostics *boundedDiagnostics
}

func (p *execProcess) PID() int {
	if p.cmd.Process == nil {
		return 0
	}
	return p.cmd.Process.Pid
}

func (p *execProcess) Wait() error {
	return p.cmd.Wait()
}

func (p *execProcess) Diagnostics() string {
	if p.diagnostics == nil {
		return ""
	}
	return p.diagnostics.String()
}

type boundedDiagnostics struct {
	mu        sync.Mutex
	data      []byte
	limit     int
	truncated bool
}

func newBoundedDiagnostics(limit int) *boundedDiagnostics {
	return &boundedDiagnostics{limit: limit}
}

func (d *boundedDiagnostics) Write(value []byte) (int, error) {
	d.mu.Lock()
	defer d.mu.Unlock()

	if d.limit <= 0 {
		d.truncated = d.truncated || len(value) > 0
		return len(value), nil
	}
	d.data = append(d.data, value...)
	if len(d.data) > d.limit {
		d.data = append([]byte(nil), d.data[len(d.data)-d.limit:]...)
		d.truncated = true
	}
	return len(value), nil
}

func (d *boundedDiagnostics) String() string {
	d.mu.Lock()
	defer d.mu.Unlock()
	value := string(d.data)
	if d.truncated {
		value = "[earlier output truncated] " + value
	}
	return value
}

var (
	secretAssignmentPattern = regexp.MustCompile(`(?i)(["']?[a-z0-9_.-]*(?:api[_-]?key|token|secret|password|passwd|credential|authorization)[a-z0-9_.-]*["']?\s*[:=]\s*)(?:["']?)[^\s,;}&"']+(?:["']?)`)
	bearerTokenPattern      = regexp.MustCompile(`(?i)(bearer\s+)[a-z0-9._~+/=-]+`)
	sensitiveQueryPattern   = regexp.MustCompile(`(?i)([?&](?:api[_-]?key|token|secret|password|passwd|credential)=)[^&\s]+`)
)

func unexpectedRunnerExitReason(process Process) string {
	reason := "The local benchmark runner exited unexpectedly."
	diagnostics, ok := process.(ProcessDiagnostics)
	if !ok {
		return reason
	}
	if details := sanitizeDiagnostics(diagnostics.Diagnostics()); details != "" {
		return reason + " Details: " + details
	}
	return reason
}

func sanitizeDiagnostics(value string) string {
	value = bearerTokenPattern.ReplaceAllString(value, "$1[REDACTED]")
	value = secretAssignmentPattern.ReplaceAllString(value, "$1[REDACTED]")
	value = sensitiveQueryPattern.ReplaceAllString(value, "$1[REDACTED]")

	var builder strings.Builder
	for _, r := range value {
		if r == '\n' || r == '\t' || unicode.IsPrint(r) {
			builder.WriteRune(r)
		}
	}
	lines := strings.FieldsFunc(builder.String(), func(r rune) bool { return r == '\n' || r == '\r' })
	value = strings.TrimSpace(strings.Join(lines, " "))
	if len(value) > 1200 {
		value = value[:1200] + "…"
	}
	return value
}

type osPrerequisites struct{}

func (osPrerequisites) LookPath(file string) (string, error) {
	return exec.LookPath(file)
}

func (osPrerequisites) WorkDirExists(path string) bool {
	info, err := os.Stat(path)
	return err == nil && info.IsDir()
}

func (osPrerequisites) DockerAvailable(ctx context.Context) error {
	docker, err := exec.LookPath("docker")
	if err != nil {
		return err
	}
	return exec.CommandContext(ctx, docker, "info", "--format", "{{.ServerVersion}}").Run()
}
