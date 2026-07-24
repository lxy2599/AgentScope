package main

import (
	"bytes"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestRunReturnsThresholdExitCodeWithoutLeakingToken(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		http.Error(w, "unavailable", http.StatusServiceUnavailable)
	}))
	defer server.Close()
	path := filepath.Join(t.TempDir(), "targets.json")
	const secret = "ascope_super_secret"
	if err := os.WriteFile(
		path,
		[]byte(`[{"project_id":"project-1","token":"`+secret+`","agent_name":"load-agent"}]`),
		0o600,
	); err != nil {
		t.Fatal(err)
	}

	var stdout, stderr bytes.Buffer
	code := run([]string{
		"--server-url", server.URL,
		"--targets", path,
		"--requests", "2",
		"--concurrency", "1",
		"--batch-size", "1",
		"--max-error-rate", "0.1",
		"--output", "json",
	}, &stdout, &stderr)

	if code != 2 {
		t.Fatalf("exit code = %d, stdout=%s stderr=%s", code, stdout.String(), stderr.String())
	}
	output := stdout.String() + stderr.String()
	if strings.Contains(output, secret) {
		t.Fatalf("output leaked token: %s", output)
	}
	if !strings.Contains(stdout.String(), `"passed": false`) {
		t.Fatalf("missing failed report: %s", stdout.String())
	}
}
