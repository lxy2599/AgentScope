package main

import "testing"

func TestReadEnvIncludesPhase4WorkerConfiguration(t *testing.T) {
	expected := map[string]string{
		"AGENTSCOPE_QUOTAS_ENABLED":               "false",
		"AGENTSCOPE_QUOTA_NAMESPACE":              "tenant-test",
		"AGENTSCOPE_QUOTA_LIMITER_TIMEOUT":        "125ms",
		"AGENTSCOPE_ANOMALY_ENABLED":              "false",
		"AGENTSCOPE_ANOMALY_INTERVAL":             "1m",
		"AGENTSCOPE_ANOMALY_BASELINE_WINDOW":      "48h",
		"AGENTSCOPE_ANOMALY_RECENT_WINDOW":        "30m",
		"AGENTSCOPE_ANOMALY_MIN_BASELINE_SAMPLES": "12",
		"AGENTSCOPE_ANOMALY_THRESHOLD":            "8.5",
	}
	for key, value := range expected {
		t.Setenv(key, value)
	}

	env := readEnv()

	for key, value := range expected {
		if env[key] != value {
			t.Fatalf("%s = %q, want %q", key, env[key], value)
		}
	}
}
