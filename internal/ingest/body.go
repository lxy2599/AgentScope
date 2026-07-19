package ingest

import (
	"compress/gzip"
	"errors"
	"fmt"
	"io"
	"net/http"
)

// Body size and span count limits.
const (
	MaxBodySize = 10 * 1024 * 1024 // 10 MB
	MaxSpans    = 5000
)

// ErrBodyTooLarge is returned when the request body exceeds MaxBodySize.
var ErrBodyTooLarge = errors.New("request body too large")

// ReadAndDecodeBody reads the HTTP request body, optionally decompressing gzip,
// and enforces the given maxSize limit on the *decompressed* content.
func ReadAndDecodeBody(r *http.Request, maxSize int) ([]byte, error) {
	if r.Body == nil {
		return nil, errors.New("request body is nil")
	}

	var reader io.Reader = r.Body

	if r.Header.Get("Content-Encoding") == "gzip" {
		gr, err := gzip.NewReader(r.Body)
		if err != nil {
			return nil, fmt.Errorf("gzip decode: %w", err)
		}
		defer gr.Close()
		reader = gr
	}

	// Read up to maxSize+1 so we can detect oversized bodies
	body, err := io.ReadAll(io.LimitReader(reader, int64(maxSize)+1))
	if err != nil {
		return nil, fmt.Errorf("read body: %w", err)
	}
	if len(body) > maxSize {
		return nil, ErrBodyTooLarge
	}
	return body, nil
}
