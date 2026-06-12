package morpheus

import (
	"errors"
	"testing"
	"time"
)

func TestIsRetryableGetErr(t *testing.T) {
	if !isRetryableGetErr(errors.New("context deadline exceeded")) {
		t.Fatal("expected retryable")
	}
	if isRetryableGetErr(errors.New("HTTP 404: not found")) {
		t.Fatal("expected not retryable")
	}
}

func TestHTTPClientTimeout_default(t *testing.T) {
	t.Setenv("MORPHEUS_SNAPSHOT_HTTP_TIMEOUT", "")
	if got := httpClientTimeout(); got != defaultHTTPTimeout {
		t.Fatalf("timeout=%v want %v", got, defaultHTTPTimeout)
	}
}

func TestHTTPClientTimeout_env(t *testing.T) {
	t.Setenv("MORPHEUS_SNAPSHOT_HTTP_TIMEOUT", "90s")
	if got := httpClientTimeout(); got != 90*time.Second {
		t.Fatalf("timeout=%v", got)
	}
}
