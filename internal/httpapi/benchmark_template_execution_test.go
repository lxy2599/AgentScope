package httpapi

import (
	"reflect"
	"testing"
)

func TestRestrictTemplateCasesPreservesRequestedOrder(t *testing.T) {
	got, err := restrictTemplateCases([]string{"case-a", "case-b", "case-c"}, []string{"case-c", "case-a"})
	if err != nil {
		t.Fatalf("restrictTemplateCases returned error: %v", err)
	}
	if want := []string{"case-c", "case-a"}; !reflect.DeepEqual(got, want) {
		t.Fatalf("selected cases = %#v, want %#v", got, want)
	}
}

func TestRestrictTemplateCasesRejectsOutsideAndDuplicateCases(t *testing.T) {
	for name, requested := range map[string][]string{
		"outside template": {"case-z"},
		"duplicate":        {"case-a", "case-a"},
		"empty":            {},
	} {
		t.Run(name, func(t *testing.T) {
			if _, err := restrictTemplateCases([]string{"case-a"}, requested); err == nil {
				t.Fatal("expected invalid selection to be rejected")
			}
		})
	}
}
