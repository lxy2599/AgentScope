package ingest

import (
	"encoding/json"
	"strings"
)

// ParseAttributes parses a JSON object string into a map.
// This is the centralized parser for all span attribute handling.
// Unlike the old extractAttr which did fragile string scanning, this
// properly handles escaped quotes, nested JSON, arrays, and all JSON types.
func ParseAttributes(attrs string) (map[string]any, error) {
	if attrs == "" {
		return nil, nil
	}
	var result map[string]any
	dec := json.NewDecoder(strings.NewReader(attrs))
	dec.UseNumber()
	if err := dec.Decode(&result); err != nil {
		return nil, err
	}
	return result, nil
}

// MustParseAttributes parses attributes and returns an empty map on error.
func MustParseAttributes(attrs string) map[string]any {
	m, _ := ParseAttributes(attrs)
	if m == nil {
		m = make(map[string]any)
	}
	return m
}

// getNestedValue navigates a map using dot-separated path parts.
// First tries the full key as-is (flat JSON keys like "llm.provider"),
// then falls back to traversing nested objects.
func getNestedValue(m map[string]any, parts []string) any {
	// Try the full joined key first (handles flat JSON keys with dots)
	fullKey := strings.Join(parts, ".")
	if v, ok := m[fullKey]; ok {
		return v
	}

	// Fall back to nested traversal (handles actual nested objects)
	current := any(m)
	for _, part := range parts {
		switch node := current.(type) {
		case map[string]any:
			current = node[part]
		default:
			return nil
		}
	}
	return current
}

// AttrString extracts a string value for the given key from the parsed map.
// Supports dot-separated keys (e.g. "llm.provider", "thought.summary").
func AttrString(m map[string]any, key string) string {
	if m == nil {
		return ""
	}
	parts := strings.Split(key, ".")
	v := getNestedValue(m, parts)
	if v == nil {
		return ""
	}
	switch val := v.(type) {
	case string:
		return val
	case json.Number:
		return val.String()
	case float64:
		return json.Number(formatFloat(val)).String()
	case bool:
		if val {
			return "true"
		}
		return "false"
	}
	return ""
}

// AttrInt extracts an int64 pointer value for the given key.
func AttrInt(m map[string]any, key string) *int64 {
	if m == nil {
		return nil
	}
	parts := strings.Split(key, ".")
	v := getNestedValue(m, parts)
	if v == nil {
		return nil
	}
	switch val := v.(type) {
	case json.Number:
		n, err := val.Int64()
		if err != nil {
			return nil
		}
		return &n
	case float64:
		n := int64(val)
		return &n
	}
	return nil
}

// AttrInt64 extracts an int64 value (with zero default) for the given key.
func AttrInt64(m map[string]any, key string) int64 {
	p := AttrInt(m, key)
	if p == nil {
		return 0
	}
	return *p
}

// AttrFloat extracts a float64 pointer value for the given key.
func AttrFloat(m map[string]any, key string) *float64 {
	if m == nil {
		return nil
	}
	parts := strings.Split(key, ".")
	v := getNestedValue(m, parts)
	if v == nil {
		return nil
	}
	switch val := v.(type) {
	case json.Number:
		n, err := val.Float64()
		if err != nil {
			return nil
		}
		return &n
	case float64:
		return &val
	}
	return nil
}

// AttrBool extracts a boolean value for the given key.
func AttrBool(m map[string]any, key string) bool {
	if m == nil {
		return false
	}
	parts := strings.Split(key, ".")
	v := getNestedValue(m, parts)
	if v == nil {
		return false
	}
	switch val := v.(type) {
	case bool:
		return val
	case string:
		return val == "true" || val == "1"
	case json.Number:
		n, err := val.Int64()
		return err == nil && n != 0
	}
	return false
}

// AttrStringArray extracts a string slice value for the given key.
func AttrStringArray(m map[string]any, key string) []string {
	if m == nil {
		return nil
	}
	parts := strings.Split(key, ".")
	v := getNestedValue(m, parts)
	if v == nil {
		return nil
	}
	switch val := v.(type) {
	case []any:
		result := make([]string, 0, len(val))
		for _, item := range val {
			if s, ok := item.(string); ok {
				result = append(result, s)
			}
		}
		return result
	}
	return nil
}

// AttrFloatArray extracts a float64 slice value for the given key.
func AttrFloatArray(m map[string]any, key string) []float64 {
	if m == nil {
		return nil
	}
	parts := strings.Split(key, ".")
	v := getNestedValue(m, parts)
	if v == nil {
		return nil
	}
	switch val := v.(type) {
	case []any:
		result := make([]float64, 0, len(val))
		for _, item := range val {
			switch f := item.(type) {
			case json.Number:
				n, err := f.Float64()
				if err == nil {
					result = append(result, n)
				}
			case float64:
				result = append(result, f)
			}
		}
		return result
	}
	return nil
}

// formatFloat formats a float64 using JSON marshaling for clean output.
func formatFloat(f float64) string {
	b, _ := json.Marshal(f)
	return string(b)
}
