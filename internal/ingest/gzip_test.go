package ingest

import (
	"bytes"
	"compress/gzip"
	"net/http/httptest"
	"testing"
)

func TestReadAndDecodeBody_gzip(t *testing.T) {
	// 准备 gzip 压缩的 body
	original := []byte(`{"key":"value"}`)
	var buf bytes.Buffer
	gw := gzip.NewWriter(&buf)
	_, _ = gw.Write(original)
	gw.Close()
	compressed := buf.Bytes()

	req := httptest.NewRequest("POST", "/", bytes.NewReader(compressed))
	req.Header.Set("Content-Encoding", "gzip")

	body, err := ReadAndDecodeBody(req, 10*1024*1024)
	if err != nil {
		t.Fatalf("ReadAndDecodeBody() error = %v", err)
	}
	if !bytes.Equal(body, original) {
		t.Errorf("got body = %v, want %v", body, original)
	}
}

func TestReadAndDecodeBody_gzipInvalidData(t *testing.T) {
	req := httptest.NewRequest("POST", "/", bytes.NewReader([]byte("not-gzip-data")))
	req.Header.Set("Content-Encoding", "gzip")

	_, err := ReadAndDecodeBody(req, 10*1024*1024)
	if err == nil {
		t.Fatal("expected error for invalid gzip data")
	}
}

func TestReadAndDecodeBody_noCompression(t *testing.T) {
	req := httptest.NewRequest("POST", "/", bytes.NewReader([]byte("hello")))
	body, err := ReadAndDecodeBody(req, 10*1024*1024)
	if err != nil {
		t.Fatalf("ReadAndDecodeBody() error = %v", err)
	}
	if string(body) != "hello" {
		t.Errorf("got body = %q, want hello", string(body))
	}
}

func TestReadAndDecodeBody_sizeLimit(t *testing.T) {
	// Body 大小刚好超过限制
	payload := make([]byte, 5*1024) // 5KB
	req := httptest.NewRequest("POST", "/", bytes.NewReader(payload))

	_, err := ReadAndDecodeBody(req, 100) // 100 bytes limit
	if err != ErrBodyTooLarge {
		t.Fatalf("expected ErrBodyTooLarge, got %v", err)
	}
}

func TestReadAndDecodeBody_gzipSizeLimit(t *testing.T) {
	// 压缩后小，但解压后超出限制
	original := make([]byte, 10000)
	var buf bytes.Buffer
	gw := gzip.NewWriter(&buf)
	_, _ = gw.Write(original)
	gw.Close()

	req := httptest.NewRequest("POST", "/", bytes.NewReader(buf.Bytes()))
	req.Header.Set("Content-Encoding", "gzip")

	_, err := ReadAndDecodeBody(req, 5000) // 原始 10KB > 5KB 限制
	if err != ErrBodyTooLarge {
		t.Fatalf("expected ErrBodyTooLarge, got %v", err)
	}
}

func TestMaxBodySize(t *testing.T) {
	// 验证常量合理性
	if MaxBodySize != 10*1024*1024 {
		t.Errorf("MaxBodySize = %d, want %d", MaxBodySize, 10*1024*1024)
	}
}

func TestMaxSpans(t *testing.T) {
	if MaxSpans != 5000 {
		t.Errorf("MaxSpans = %d, want %d", MaxSpans, 5000)
	}
}

func TestErrBodyTooLargeIsSentinel(t *testing.T) {
	if ErrBodyTooLarge == nil {
		t.Fatal("ErrBodyTooLarge should be a non-nil error")
	}
	if ErrBodyTooLarge.Error() == "" {
		t.Fatal("ErrBodyTooLarge.Error() should not be empty")
	}
}

func TestReadBodyLimit(t *testing.T) {
	// 验证 LimitReader 在 ReadAndDecodeBody 中正确工作
	bigBody := make([]byte, 100)
	req := httptest.NewRequest("POST", "/", bytes.NewReader(bigBody))
	body, err := ReadAndDecodeBody(req, 1000) // limit > body
	if err != nil {
		t.Fatalf("ReadAndDecodeBody() error = %v", err)
	}
	if len(body) != 100 {
		t.Errorf("body length = %d, want 100", len(body))
	}

	// body 大小在边界上
	exactBody := make([]byte, 100)
	req2 := httptest.NewRequest("POST", "/", bytes.NewReader(exactBody))
	body2, err := ReadAndDecodeBody(req2, 100) // limit == body
	if err != nil {
		t.Fatalf("ReadAndDecodeBody() error = %v", err)
	}
	if len(body2) != 100 {
		t.Errorf("body2 length = %d, want 100", len(body2))
	}
}
