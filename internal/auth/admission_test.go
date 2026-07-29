package auth

import (
	"testing"
	"time"
)

func restrictiveAdmissionOptions() AdmissionOptions {
	return AdmissionOptions{
		Enabled:              true,
		LoginIP:              RatePolicy{Capacity: 2, Refill: 2, Interval: time.Minute},
		LoginPrincipal:       RatePolicy{Capacity: 2, Refill: 2, Interval: time.Minute},
		RegisterIP:           RatePolicy{Capacity: 1, Refill: 1, Interval: time.Minute},
		RegisterPrincipal:    RatePolicy{Capacity: 1, Refill: 1, Interval: time.Minute},
		BcryptMaxConcurrent:  1,
		MaxTrackedPrincipals: 10,
	}
}

func TestAdmissionControllerLimitsIPAndPrincipalWithoutStoringRawEmail(t *testing.T) {
	now := time.Date(2026, 7, 29, 1, 0, 0, 0, time.UTC)
	controller := NewAdmissionController(restrictiveAdmissionOptions())
	controller.now = func() time.Time { return now }

	for i := 0; i < 2; i++ {
		release, outcome, _ := controller.Admit("login", "203.0.113.0/24", "Alice@Example.com")
		if outcome != AdmissionAllowed {
			t.Fatalf("attempt %d outcome = %s, want allowed", i, outcome)
		}
		release()
	}
	if _, outcome, retry := controller.Admit("login", "203.0.113.0/24", "alice@example.com"); outcome != AdmissionRateLimited || retry <= 0 {
		t.Fatalf("third attempt outcome=%s retry=%s, want rate limited", outcome, retry)
	}
	for key := range controller.buckets {
		if key == "alice@example.com" || key == "Alice@Example.com" {
			t.Fatalf("raw principal leaked into bucket key %q", key)
		}
	}
}

func TestAdmissionControllerSeparatesIPAndPrincipalPolicies(t *testing.T) {
	now := time.Date(2026, 7, 29, 1, 0, 0, 0, time.UTC)
	options := restrictiveAdmissionOptions()
	options.LoginIP = RatePolicy{Capacity: 1, Refill: 1, Interval: time.Minute}
	options.LoginPrincipal = RatePolicy{Capacity: 10, Refill: 10, Interval: time.Minute}
	controller := NewAdmissionController(options)
	controller.now = func() time.Time { return now }

	release, outcome, _ := controller.Admit("login", "203.0.113.0/24", "one@example.com")
	if outcome != AdmissionAllowed {
		t.Fatalf("first outcome = %s", outcome)
	}
	release()
	if _, outcome, _ := controller.Admit("login", "203.0.113.0/24", "two@example.com"); outcome != AdmissionRateLimited {
		t.Fatalf("same IP with another principal outcome = %s, want rate limited", outcome)
	}
}

func TestAdmissionControllerRejectsWhenBcryptCapacityIsBusy(t *testing.T) {
	options := restrictiveAdmissionOptions()
	options.LoginIP = RatePolicy{}
	options.LoginPrincipal = RatePolicy{}
	controller := NewAdmissionController(options)

	release, outcome, _ := controller.Admit("login", "203.0.113.0/24", "user@example.com")
	if outcome != AdmissionAllowed {
		t.Fatalf("first outcome = %s", outcome)
	}
	if _, outcome, retry := controller.Admit("login", "198.51.100.0/24", "other@example.com"); outcome != AdmissionBusy || retry <= 0 {
		t.Fatalf("second outcome=%s retry=%s, want busy", outcome, retry)
	}
	release()
	if release, outcome, _ := controller.Admit("login", "198.51.100.0/24", "other@example.com"); outcome != AdmissionAllowed {
		t.Fatalf("released outcome = %s", outcome)
	} else {
		release()
	}
}

func TestAdmissionControllerRefillsOverTime(t *testing.T) {
	now := time.Date(2026, 7, 29, 1, 0, 0, 0, time.UTC)
	options := restrictiveAdmissionOptions()
	options.LoginIP = RatePolicy{Capacity: 1, Refill: 1, Interval: time.Second}
	options.LoginPrincipal = RatePolicy{}
	controller := NewAdmissionController(options)
	controller.now = func() time.Time { return now }

	release, outcome, _ := controller.Admit("login", "203.0.113.0/24", "user@example.com")
	if outcome != AdmissionAllowed {
		t.Fatal(outcome)
	}
	release()
	if _, outcome, _ := controller.Admit("login", "203.0.113.0/24", "user@example.com"); outcome != AdmissionRateLimited {
		t.Fatalf("outcome = %s, want rate limited", outcome)
	}
	now = now.Add(time.Second)
	if release, outcome, _ := controller.Admit("login", "203.0.113.0/24", "user@example.com"); outcome != AdmissionAllowed {
		t.Fatalf("refilled outcome = %s", outcome)
	} else {
		release()
	}
}
