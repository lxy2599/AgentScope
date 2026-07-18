package adapter

import (
	"testing"
)

func TestNormalizeResultDetailsDefaultsEmptyJSON(t *testing.T) {
	got := normalizeResultDetails(nil)
	if string(got) != "{}" {
		t.Fatalf("normalizeResultDetails(nil) = %q, want {}", string(got))
	}

	got = normalizeResultDetails([]byte{})
	if string(got) != "{}" {
		t.Fatalf("normalizeResultDetails(empty) = %q, want {}", string(got))
	}
}

func TestNormalizeResultDetailsKeepsProvidedJSON(t *testing.T) {
	got := normalizeResultDetails([]byte(`{"score":1}`))
	if string(got) != `{"score":1}` {
		t.Fatalf("normalizeResultDetails(provided) = %q", string(got))
	}
}

func TestNumericFromFloat64_zero(t *testing.T) {
	n, err := numericFromFloat64(0.0)
	if err != nil {
		t.Fatalf("numericFromFloat64(0.0) error = %v", err)
	}
	if !n.Valid {
		t.Fatal("numericFromFloat64(0.0) is not Valid")
	}
	f, _ := n.Float64Value()
	if f.Float64 != 0.0 {
		t.Fatalf("numericFromFloat64(0.0) = %v, want 0.0", f.Float64)
	}
}

func TestNumericFromFloat64_one(t *testing.T) {
	n, err := numericFromFloat64(1.0)
	if err != nil {
		t.Fatalf("numericFromFloat64(1.0) error = %v", err)
	}
	if !n.Valid {
		t.Fatal("numericFromFloat64(1.0) is not Valid")
	}
	f, _ := n.Float64Value()
	if f.Float64 != 1.0 {
		t.Fatalf("numericFromFloat64(1.0) = %v, want 1.0", f.Float64)
	}
}

func TestNumericFromFloat64_decimal(t *testing.T) {
	n, err := numericFromFloat64(0.95)
	if err != nil {
		t.Fatalf("numericFromFloat64(0.95) error = %v", err)
	}
	if !n.Valid {
		t.Fatal("numericFromFloat64(0.95) is not Valid")
	}
	f, _ := n.Float64Value()
	if f.Float64 != 0.95 {
		t.Fatalf("numericFromFloat64(0.95) = %v, want 0.95", f.Float64)
	}
}

func TestNumericFromFloat64Ptr_zero_returns_invalid(t *testing.T) {
	n, err := numericFromFloat64Ptr(0.0)
	if err != nil {
		t.Fatalf("numericFromFloat64Ptr(0.0) error = %v", err)
	}
	if n.Valid {
		t.Fatal("numericFromFloat64Ptr(0.0) should be invalid (NULL)")
	}
}

func TestNumericFromFloat64Ptr_nonzero_is_valid(t *testing.T) {
	n, err := numericFromFloat64Ptr(42.5)
	if err != nil {
		t.Fatalf("numericFromFloat64Ptr(42.5) error = %v", err)
	}
	if !n.Valid {
		t.Fatal("numericFromFloat64Ptr(42.5) is not Valid")
	}
	f, _ := n.Float64Value()
	if f.Float64 != 42.5 {
		t.Fatalf("numericFromFloat64Ptr(42.5) = %v, want 42.5", f.Float64)
	}
}
