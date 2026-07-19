package ingest

import (
	"testing"
	"time"
)

func TestTokenBucketBasic(t *testing.T) {
	// 容量 3，每秒补充 1 个
	tb := NewTokenBucket(3, 1, time.Second)

	// 初始应能获取 3 个
	if !tb.Allow() {
		t.Error("expected Allow() to return true for first token")
	}
	if !tb.Allow() {
		t.Error("expected Allow() to return true for second token")
	}
	if !tb.Allow() {
		t.Error("expected Allow() to return true for third token")
	}
	// 第 4 个应该失败（容量耗尽）
	if tb.Allow() {
		t.Error("expected Allow() to return false when bucket is empty")
	}
}

func TestTokenBucketAllowN(t *testing.T) {
	tb := NewTokenBucket(5, 5, time.Second)

	if !tb.AllowN(5) {
		t.Error("expected AllowN(5) to return true")
	}
	if tb.AllowN(1) {
		t.Error("expected AllowN(1) to return false after capacity exhausted")
	}
}

func TestTokenBucketRefill(t *testing.T) {
	tb := NewTokenBucket(2, 2, 50*time.Millisecond)

	if !tb.AllowN(2) {
		t.Error("expected AllowN(2) to consume all tokens")
	}
	if tb.Allow() {
		t.Error("expected Allow() to fail after consumption")
	}

	// 等待 refill
	time.Sleep(60 * time.Millisecond)

	if !tb.Allow() {
		t.Error("expected Allow() to succeed after refill")
	}
}

func TestProjectRateLimiterPerProjectIsolation(t *testing.T) {
	prl := NewProjectRateLimiter(1, 1, time.Second)

	// project A 用掉唯一 token
	if !prl.Allow("project-a", 1) {
		t.Error("expected project-a first request to be allowed")
	}
	// project A 再次请求应被限流
	if prl.Allow("project-a", 1) {
		t.Error("expected project-a second request to be rate limited")
	}
	// project B 应该不受影响
	if !prl.Allow("project-b", 1) {
		t.Error("expected project-b to be isolated from project-a")
	}
}

func TestProjectRateLimiterAllowN(t *testing.T) {
	prl := NewProjectRateLimiter(5, 5, time.Second)

	// 请求 3 个 token，应成功
	if !prl.Allow("project-x", 3) {
		t.Error("expected AllowN(3) to succeed with capacity 5")
	}
	// 再请求 3 个，应失败（只剩 2）
	if prl.Allow("project-x", 3) {
		t.Error("expected AllowN(3) to fail with only 2 tokens left")
	}
}

func TestNewProjectRateLimiterDefaults(t *testing.T) {
	prl := NewProjectRateLimiter(100, 100, time.Second)

	if prl.capacity != 100 {
		t.Errorf("expected capacity 100, got %d", prl.capacity)
	}
	if prl.refill != 100 {
		t.Errorf("expected refill 100, got %d", prl.refill)
	}
}
