package benchlauncher

import (
	"context"
	"errors"
	"strings"
	"sync"
	"testing"
	"time"
)

type fakeTokenIssuer struct {
	mu          sync.Mutex
	createCalls int
	revokeCalls int
	expiresAt   time.Time
}

func (f *fakeTokenIssuer) CreateRunnerToken(_ context.Context, _, _ string, expiresAt time.Time) (string, string, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.createCalls++
	f.expiresAt = expiresAt
	return "internal-project-token", "token-1", nil
}

func (f *fakeTokenIssuer) RevokeRunnerToken(context.Context, string, string, string, string) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.revokeCalls++
	return nil
}

type fakePrerequisites struct {
	executableErr error
	workDir       bool
	dockerErr     error
}

func (f fakePrerequisites) LookPath(string) (string, error) {
	if f.executableErr != nil {
		return "", f.executableErr
	}
	return "/usr/bin/uv", nil
}

func (f fakePrerequisites) WorkDirExists(string) bool {
	return f.workDir
}

func (f fakePrerequisites) DockerAvailable(context.Context) error {
	return f.dockerErr
}

type fakeProcess struct {
	pid  int
	done chan struct{}
	once sync.Once
	err  error
}

func newFakeProcess(pid int) *fakeProcess {
	return &fakeProcess{pid: pid, done: make(chan struct{})}
}

func (p *fakeProcess) PID() int {
	return p.pid
}

func (p *fakeProcess) Wait() error {
	<-p.done
	return p.err
}

func (p *fakeProcess) Finish(err error) {
	p.err = err
	p.once.Do(func() { close(p.done) })
}

type fakeProcessStarter struct {
	mu       sync.Mutex
	commands []Command
	process  *fakeProcess
	err      error
}

func (f *fakeProcessStarter) Start(_ context.Context, command Command) (Process, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.commands = append(f.commands, command)
	if f.err != nil {
		return nil, f.err
	}
	return f.process, nil
}

func testConfig() Config {
	return Config{
		Enabled:       true,
		Executable:    "uv",
		WorkDir:       "/repo/python",
		HarborWorkDir: "/repo/.external/harbor",
		ServerURL:     "http://127.0.0.1:8080",
		ArkAPIKey:     "server-only-key",
		ArkBaseURL:    "https://ark.example.test/v3",
		Model:         "ark-code-latest",
		TokenTTL:      time.Hour,
	}
}

func TestLaunchStartsOneRunnerAndInjectsSafeEnvironment(t *testing.T) {
	t.Setenv("ARK_API_KEY", "parent-key-must-not-leak")
	t.Setenv("OPENAI_API_KEY", "parent-openai-key-must-not-leak")
	tokens := &fakeTokenIssuer{}
	process := newFakeProcess(4321)
	starter := &fakeProcessStarter{process: process}
	launcher := New(testConfig(), tokens, WithProcessStarter(starter), WithPrerequisites(fakePrerequisites{workDir: true}), WithClock(func() time.Time {
		return time.Date(2026, 8, 9, 12, 0, 0, 0, time.UTC)
	}))

	first, err := launcher.Launch(context.Background(), LaunchRequest{ProjectID: "project-1", UserID: "admin-1", ExecutionKind: "harbor"})
	if err != nil {
		t.Fatalf("Launch() error = %v", err)
	}
	if first.State != "running" || first.PID != 4321 {
		t.Fatalf("first launch status = %#v", first)
	}
	second, err := launcher.Launch(context.Background(), LaunchRequest{ProjectID: "project-1", UserID: "admin-1", ExecutionKind: "harbor"})
	if err != nil {
		t.Fatalf("duplicate Launch() error = %v", err)
	}
	if !second.AlreadyRunning || second.PID != 4321 {
		t.Fatalf("duplicate launch status = %#v", second)
	}

	starter.mu.Lock()
	if len(starter.commands) != 1 {
		t.Fatalf("runner starts = %d, want 1", len(starter.commands))
	}
	command := starter.commands[0]
	starter.mu.Unlock()
	if command.Executable != "uv" || command.Dir != "/repo" {
		t.Fatalf("command = %#v", command)
	}
	wantArgs := []string{"run", "--directory", "/repo/python", "--frozen", "agentscope-runner", "start", "--execution-kind", "harbor"}
	if strings.Join(command.Args, "\x00") != strings.Join(wantArgs, "\x00") {
		t.Fatalf("command args = %#v, want %#v", command.Args, wantArgs)
	}
	env := environmentMap(command.Env)
	if env["AGENTSCOPE_PROJECT_TOKEN"] != "internal-project-token" || env["OPENAI_API_KEY"] != "server-only-key" || env["OPENAI_BASE_URL"] != "https://ark.example.test/v3" || env["AGENTSCOPE_BENCHMARK_MODEL"] != "ark-code-latest" || env["AGENTSCOPE_HARBOR_WORKDIR"] != "/repo/.external/harbor" {
		t.Fatalf("runner environment missing required values: %#v", env)
	}
	if _, found := env["ARK_API_KEY"]; found {
		t.Fatal("ARK_API_KEY leaked into runner environment")
	}

	tokens.mu.Lock()
	if tokens.createCalls != 1 || !tokens.expiresAt.Equal(time.Date(2026, 8, 9, 13, 0, 0, 0, time.UTC)) {
		t.Fatalf("token issuer calls=%d expires_at=%s", tokens.createCalls, tokens.expiresAt)
	}
	tokens.mu.Unlock()

	process.Finish(nil)
	eventually(t, func() bool {
		tokens.mu.Lock()
		defer tokens.mu.Unlock()
		return tokens.revokeCalls == 1
	})
}

func TestLaunchBlocksWhenConfigurationOrDockerIsUnavailable(t *testing.T) {
	tokens := &fakeTokenIssuer{}
	starter := &fakeProcessStarter{process: newFakeProcess(1)}
	disabled := testConfig()
	disabled.Enabled = false
	disabled.ArkAPIKey = ""
	launcher := New(disabled, tokens, WithProcessStarter(starter), WithPrerequisites(fakePrerequisites{workDir: true}))
	status, err := launcher.Launch(context.Background(), LaunchRequest{ProjectID: "project-1", UserID: "admin-1", ExecutionKind: "tau2"})
	if !errors.Is(err, ErrUnavailable) || status.State != "blocked" || len(status.Blockers) < 2 {
		t.Fatalf("disabled launch = (%#v, %v)", status, err)
	}

	launcher = New(testConfig(), tokens, WithProcessStarter(starter), WithPrerequisites(fakePrerequisites{workDir: true, dockerErr: errors.New("daemon unavailable")}))
	status, err = launcher.Launch(context.Background(), LaunchRequest{ProjectID: "project-1", UserID: "admin-1", ExecutionKind: "harbor"})
	if !errors.Is(err, ErrUnavailable) || !strings.Contains(status.Reason, "Docker") {
		t.Fatalf("docker-blocked launch = (%#v, %v)", status, err)
	}
	starter.mu.Lock()
	starts := len(starter.commands)
	starter.mu.Unlock()
	if starts != 0 {
		t.Fatalf("runner starts = %d, want 0 when blocked", starts)
	}
}

func TestLaunchRevokesTokenWhenProcessCannotStart(t *testing.T) {
	tokens := &fakeTokenIssuer{}
	starter := &fakeProcessStarter{err: errors.New("uv unavailable")}
	launcher := New(testConfig(), tokens, WithProcessStarter(starter), WithPrerequisites(fakePrerequisites{workDir: true}))
	if _, err := launcher.Launch(context.Background(), LaunchRequest{ProjectID: "project-1", UserID: "admin-1", ExecutionKind: "tau2"}); err == nil {
		t.Fatal("Launch() error = nil, want start failure")
	}
	tokens.mu.Lock()
	defer tokens.mu.Unlock()
	if tokens.createCalls != 1 || tokens.revokeCalls != 1 {
		t.Fatalf("token create/revoke = %d/%d, want 1/1", tokens.createCalls, tokens.revokeCalls)
	}
}

func TestRunnerEnvironmentReplacesParentCredentialValues(t *testing.T) {
	env := environmentMap(runnerEnvironment([]string{
		"PATH=/usr/bin",
		"ARK_API_KEY=parent-ark",
		"OPENAI_API_KEY=parent-openai",
		"AGENTSCOPE_PROJECT_TOKEN=parent-project-token",
		"OPENHANDS_AGENTSCOPE_ENABLED=false",
	}, map[string]string{
		"OPENAI_API_KEY":                     "server-ark",
		"AGENTSCOPE_PROJECT_TOKEN":           "ephemeral-project-token",
		"OPENHANDS_AGENTSCOPE_ENABLED":       "true",
		"OPENHANDS_AGENTSCOPE_CAPTURE_LEVEL": "L0",
	}))
	if env["PATH"] != "/usr/bin" || env["OPENAI_API_KEY"] != "server-ark" || env["AGENTSCOPE_PROJECT_TOKEN"] != "ephemeral-project-token" || env["OPENHANDS_AGENTSCOPE_ENABLED"] != "true" || env["OPENHANDS_AGENTSCOPE_CAPTURE_LEVEL"] != "L0" {
		t.Fatalf("runner environment = %#v", env)
	}
	if _, found := env["ARK_API_KEY"]; found {
		t.Fatal("parent ARK_API_KEY remained in runner environment")
	}
}

func environmentMap(entries []string) map[string]string {
	result := make(map[string]string, len(entries))
	for _, entry := range entries {
		key, value, found := strings.Cut(entry, "=")
		if found {
			result[key] = value
		}
	}
	return result
}

func eventually(t *testing.T, condition func() bool) {
	t.Helper()
	deadline := time.Now().Add(time.Second)
	for time.Now().Before(deadline) {
		if condition() {
			return
		}
		time.Sleep(time.Millisecond)
	}
	t.Fatal("condition did not become true")
}
