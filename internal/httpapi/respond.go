package httpapi

import (
	"encoding/json"
	"net/http"
)

// writeJSON encodes payload as JSON and writes it with the given status code.
// It sets Content-Type to application/json.
func writeJSON(w http.ResponseWriter, status int, payload any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	if payload != nil {
		json.NewEncoder(w).Encode(payload)
	}
}

// writeError writes a structured JSON error response.
// The response body contains "code" and "message" fields.
func writeError(w http.ResponseWriter, status int, code string, message string) {
	writeJSON(w, status, map[string]string{
		"code":    code,
		"message": message,
	})
}
