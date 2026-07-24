package evaluators

import "testing"

func TestValidateRubric(t *testing.T) {
	valid := Rubric{
		Dimensions: []RubricDimension{
			{Name: "accuracy", Weight: 0.6, Essential: true},
			{Name: "safety", Weight: 0.4, Essential: true, Veto: true},
		},
	}
	if err := ValidateRubric(valid); err != nil {
		t.Fatalf("valid rubric rejected: %v", err)
	}

	tests := []struct {
		name   string
		rubric Rubric
	}{
		{
			name: "weights must sum to one",
			rubric: Rubric{Dimensions: []RubricDimension{
				{Name: "accuracy", Weight: 0.7},
				{Name: "safety", Weight: 0.4},
			}},
		},
		{
			name: "essential dimension needs threshold",
			rubric: Rubric{Dimensions: []RubricDimension{
				{Name: "accuracy", Weight: 1, Essential: true, PassThreshold: -1},
			}},
		},
		{
			name: "veto dimension must be essential",
			rubric: Rubric{Dimensions: []RubricDimension{
				{Name: "safety", Weight: 1, Veto: true},
			}},
		},
		{
			name: "duplicate dimension",
			rubric: Rubric{Dimensions: []RubricDimension{
				{Name: "quality", Weight: 0.5},
				{Name: "quality", Weight: 0.5},
			}},
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if err := ValidateRubric(tt.rubric); err == nil {
				t.Fatal("expected validation error")
			}
		})
	}
}
