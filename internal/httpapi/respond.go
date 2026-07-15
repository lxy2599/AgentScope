package httpapi

import (
	"encoding/json"
	"errors"
	"io"
	"net/http"
)

// Sentinel errors for handler-level responses. These are compared by the
// caller (not returned to the client) to distinguish error types.
var (
	errUnauthorized = errors.New("unauthorized")
	errForbidden   = errors.New("forbidden")
	errNotFound    = errors.New("not found")
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

// decodeJSON reads the request body and decodes JSON into the given target.
// It returns an error if the body is not valid JSON.
func decodeJSON(r *http.Request, target any) error {
	defer r.Body.Close()
	return json.NewDecoder(r.Body).Decode(target)
}

// readBody reads the full request body, limiting to maxSize bytes.
func readBody(r *http.Request, maxSize int64) ([]byte, error) {
	defer r.Body.Close()
	if maxSize <= 0 {
		maxSize = 10 * 1024 * 1024 // default 10MB
	}
	body, err := io.ReadAll(io.LimitReader(r.Body, maxSize+1))
	if err != nil {
		return nil, err
	}
	if int64(len(body)) > maxSize {
		return nil, errors.New("body too large")
	}
	return body, nil
}
