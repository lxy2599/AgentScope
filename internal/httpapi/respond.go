package httpapi

import (
	"bytes"
	"encoding/json"
	"errors"
	"io"
	"log"
	"net/http"
)

const maxJSONBodySize = 4 << 20

// Sentinel errors for handler-level responses. These are compared by the
// caller (not returned to the client) to distinguish error types.
var (
	errNotFound           = errors.New("not found")
	errJSONBodyTooLarge   = errors.New("JSON body too large")
	errMultipleJSONValues = errors.New("request body must contain a single JSON value")
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

// writeInternalError logs an internal dependency error and returns only a
// stable, non-sensitive error to the client.
func writeInternalError(w http.ResponseWriter, code, message, operation string, err error) {
	// Do not log err.Error() here. Dependency errors can contain DSNs,
	// Authorization headers, SQL fragments, filesystem paths, or payload
	// content. The operation and concrete error type retain a safe diagnostic
	// signal without turning server logs into another exfiltration channel.
	log.Printf("httpapi internal error operation=%q error_type=%T", operation, err)
	writeError(w, http.StatusInternalServerError, code, message)
}

// decodeJSONRequest decodes a required JSON request body and writes a
// consistent client response when decoding fails.
func decodeJSONRequest(w http.ResponseWriter, r *http.Request, target any) bool {
	return decodeJSONRequestWithOptions(w, r, target, false)
}

// decodeOptionalJSONRequest accepts an empty body, but any non-empty body must
// contain exactly one valid JSON value and respect the body-size limit.
func decodeOptionalJSONRequest(w http.ResponseWriter, r *http.Request, target any) bool {
	return decodeJSONRequestWithOptions(w, r, target, true)
}

func decodeJSONRequestWithOptions(w http.ResponseWriter, r *http.Request, target any, optional bool) bool {
	err := decodeJSONWithOptions(r, target, optional)
	if err == nil {
		return true
	}
	if errors.Is(err, errJSONBodyTooLarge) {
		writeError(w, http.StatusRequestEntityTooLarge, "body_too_large", "Request body exceeds maximum size")
		return false
	}
	writeError(w, http.StatusBadRequest, "invalid_request", "Invalid JSON body")
	return false
}

// decodeJSON reads the request body and decodes JSON into the given target.
// It bounds memory use and rejects trailing JSON values.
func decodeJSON(r *http.Request, target any) error {
	return decodeJSONWithOptions(r, target, false)
}

func decodeJSONWithOptions(r *http.Request, target any, optional bool) error {
	defer r.Body.Close()

	body, err := io.ReadAll(io.LimitReader(r.Body, maxJSONBodySize+1))
	if err != nil {
		return err
	}
	if len(body) > maxJSONBodySize {
		return errJSONBodyTooLarge
	}
	if len(bytes.TrimSpace(body)) == 0 {
		if optional {
			return nil
		}
		return io.EOF
	}

	decoder := json.NewDecoder(bytes.NewReader(body))
	if err := decoder.Decode(target); err != nil {
		return err
	}

	var extra any
	if err := decoder.Decode(&extra); err != io.EOF {
		if err == nil {
			return errMultipleJSONValues
		}
		return err
	}
	return nil
}
