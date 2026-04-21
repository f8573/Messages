package main

import (
	"context"
	"errors"
	"testing"
	"time"
)

func TestRetryUntilReadyRetriesThenSucceeds(t *testing.T) {
	t.Cleanup(func() {
		startupRetryDelay = time.Second
		startupAttemptTimeout = 3 * time.Second
	})

	startupRetryDelay = time.Millisecond
	startupAttemptTimeout = 20 * time.Millisecond

	attempts := 0
	err := retryUntilReady(context.Background(), "redis", func(context.Context) error {
		attempts += 1
		if attempts < 3 {
			return errors.New("not ready")
		}
		return nil
	})
	if err != nil {
		t.Fatalf("retryUntilReady returned error: %v", err)
	}
	if attempts != 3 {
		t.Fatalf("expected 3 attempts, got %d", attempts)
	}
}

func TestRetryUntilReadyStopsOnContextCancel(t *testing.T) {
	t.Cleanup(func() {
		startupRetryDelay = time.Second
		startupAttemptTimeout = 3 * time.Second
	})

	startupRetryDelay = time.Millisecond
	startupAttemptTimeout = 20 * time.Millisecond

	ctx, cancel := context.WithCancel(context.Background())
	cancel()

	err := retryUntilReady(ctx, "redis", func(context.Context) error {
		return errors.New("not ready")
	})
	if err == nil {
		t.Fatal("expected context cancellation error")
	}
}
