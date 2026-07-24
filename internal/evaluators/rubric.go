package evaluators

import (
	"errors"
	"fmt"
	"math"
)

// RubricDimension is one independently scored criterion.
type RubricDimension struct {
	Name          string  `json:"name"`
	Description   string  `json:"description,omitempty"`
	Weight        float64 `json:"weight"`
	PassThreshold float64 `json:"pass_threshold"`
	Essential     bool    `json:"essential,omitempty"`
	Veto          bool    `json:"veto,omitempty"`
}

// Rubric is the immutable payload of a published rubric version.
type Rubric struct {
	Dimensions []RubricDimension `json:"dimensions"`
}

// ValidateRubric validates invariants needed for deterministic aggregation.
func ValidateRubric(r Rubric) error {
	if len(r.Dimensions) == 0 {
		return errors.New("rubric must contain at least one dimension")
	}
	names := make(map[string]struct{}, len(r.Dimensions))
	totalWeight := 0.0
	for _, dimension := range r.Dimensions {
		if dimension.Name == "" {
			return errors.New("rubric dimension name is required")
		}
		if _, exists := names[dimension.Name]; exists {
			return fmt.Errorf("duplicate rubric dimension %q", dimension.Name)
		}
		names[dimension.Name] = struct{}{}
		if dimension.Weight <= 0 || dimension.Weight > 1 {
			return fmt.Errorf("dimension %q weight must be in (0, 1]", dimension.Name)
		}
		if dimension.PassThreshold < 0 || dimension.PassThreshold > 1 {
			return fmt.Errorf("dimension %q pass_threshold must be in [0, 1]", dimension.Name)
		}
		if dimension.Veto && !dimension.Essential {
			return fmt.Errorf("veto dimension %q must also be essential", dimension.Name)
		}
		totalWeight += dimension.Weight
	}
	if math.Abs(totalWeight-1) > 0.000001 {
		return fmt.Errorf("rubric dimension weights must sum to 1, got %.6f", totalWeight)
	}
	return nil
}
