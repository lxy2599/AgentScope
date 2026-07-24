package main

import "testing"

func TestReadEnvIncludesIngestConfiguration(t *testing.T) {
	want := map[string]string{
		"AGENTSCOPE_INGEST_MAX_BODY_SIZE":       "1234",
		"AGENTSCOPE_INGEST_MAX_SPANS":           "4321",
		"AGENTSCOPE_INGEST_RATE_LIMIT_ENABLED":  "true",
		"AGENTSCOPE_INGEST_RATE_LIMIT_CAPACITY": "25",
		"AGENTSCOPE_INGEST_RATE_LIMIT_REFILL":   "10",
		"AGENTSCOPE_INGEST_RATE_LIMIT_INTERVAL": "250ms",
	}
	for key, value := range want {
		t.Setenv(key, value)
	}

	got := readEnv()
	for key, wantValue := range want {
		if got[key] != wantValue {
			t.Errorf("readEnv()[%q] = %q, want %q", key, got[key], wantValue)
		}
	}
}
