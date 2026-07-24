package httpapi

import (
	"bytes"
	"errors"
	"log"
	"net/http/httptest"
	"strings"
	"testing"
)

func TestDecodeJSONRejectsOversizedBody(t *testing.T) {
	body := `{"value":"` + strings.Repeat("x", maxJSONBodySize) + `"}`
	req := httptest.NewRequest("POST", "/", strings.NewReader(body))

	var target map[string]string
	err := decodeJSON(req, &target)
	if !errors.Is(err, errJSONBodyTooLarge) {
		t.Fatalf("decodeJSON() error = %v, want errJSONBodyTooLarge", err)
	}
}

func TestDecodeJSONRejectsMultipleValues(t *testing.T) {
	req := httptest.NewRequest("POST", "/", strings.NewReader(`{"value":"first"} {"value":"second"}`))

	var target map[string]string
	if err := decodeJSON(req, &target); err == nil {
		t.Fatal("decodeJSON() accepted multiple JSON values")
	}
}

func TestDecodeJSONAcceptsSingleValue(t *testing.T) {
	req := httptest.NewRequest("POST", "/", strings.NewReader(`{"value":"ok"}`))

	var target map[string]string
	if err := decodeJSON(req, &target); err != nil {
		t.Fatalf("decodeJSON() error = %v", err)
	}
	if target["value"] != "ok" {
		t.Fatalf("decoded value = %q, want ok", target["value"])
	}
}

func TestWriteInternalErrorDoesNotLeakErrorTextToLogs(t *testing.T) {
	var logs bytes.Buffer
	previousOutput := log.Writer()
	previousFlags := log.Flags()
	log.SetOutput(&logs)
	log.SetFlags(0)
	t.Cleanup(func() {
		log.SetOutput(previousOutput)
		log.SetFlags(previousFlags)
	})

	rec := httptest.NewRecorder()
	writeInternalError(
		rec,
		"dependency_failed",
		"Dependency failed",
		"load resource",
		errors.New(
			"postgres://user:password@db/private?token=secret "+
				"Authorization: Bearer ascope_sensitive",
		),
	)

	if strings.Contains(logs.String(), "password") ||
		strings.Contains(logs.String(), "secret") ||
		strings.Contains(logs.String(), "ascope_sensitive") {
		t.Fatalf("internal error log leaked dependency text: %s", logs.String())
	}
	if !strings.Contains(logs.String(), `operation="load resource"`) ||
		!strings.Contains(logs.String(), "error_type=*errors.errorString") {
		t.Fatalf("internal error log lacks safe diagnostics: %s", logs.String())
	}
}
