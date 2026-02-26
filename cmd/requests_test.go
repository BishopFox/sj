package cmd

import (
	"context"
	"net/url"
	"testing"
	"time"

	"golang.org/x/time/rate"
)

// TestInitRateLimiter_Unlimited verifies that rate limiting is disabled when requestsPerSecond <= 0
func TestInitRateLimiter_Unlimited(t *testing.T) {
	// Test with 0 (unlimited)
	InitRateLimiter(0)
	if limiter != nil {
		t.Error("Expected limiter to be nil for unlimited mode (0 req/sec), but it was not nil")
	}

	// Test with negative value (unlimited)
	InitRateLimiter(-5)
	if limiter != nil {
		t.Error("Expected limiter to be nil for unlimited mode (negative req/sec), but it was not nil")
	}
}

func TestEndpointForDangerousCheck_UsesPathAndQuery(t *testing.T) {
	u, err := url.Parse("https://example.com/v1/delete-user?id=1")
	if err != nil {
		t.Fatalf("failed to parse URL: %v", err)
	}

	got := endpointForDangerousCheck(u)
	if got != "/v1/delete-user?id=1" {
		t.Fatalf("unexpected endpoint string: %s", got)
	}
}

// TestInitRateLimiter_Limited verifies that a valid rate limiter is created for positive values
func TestInitRateLimiter_Limited(t *testing.T) {
	testCases := []int{1, 5, 10, 100}

	for _, requestsPerSecond := range testCases {
		InitRateLimiter(requestsPerSecond)
		if limiter == nil {
			t.Errorf("Expected limiter to be non-nil for %d req/sec, but got nil", requestsPerSecond)
			continue
		}

		// Verify the limiter has the correct rate
		expectedLimit := rate.Limit(requestsPerSecond)
		if limiter.Limit() != expectedLimit {
			t.Errorf("Expected limiter rate to be %v, got %v", expectedLimit, limiter.Limit())
		}

		// Verify burst is 1 (no bursting allowed)
		if limiter.Burst() != 1 {
			t.Errorf("Expected limiter burst to be 1, got %d", limiter.Burst())
		}
	}
}

// TestWaitForRateLimit_Unlimited verifies immediate return when limiter is nil
func TestWaitForRateLimit_Unlimited(t *testing.T) {
	InitRateLimiter(0) // Set to unlimited mode

	ctx := context.Background()
	start := time.Now()
	err := WaitForRateLimit(ctx)
	elapsed := time.Since(start)

	if err != nil {
		t.Errorf("Expected no error from WaitForRateLimit in unlimited mode, got: %v", err)
	}

	// Should return almost immediately (allow 10ms for test overhead)
	if elapsed > 10*time.Millisecond {
		t.Errorf("Expected immediate return in unlimited mode, but took %v", elapsed)
	}
}

// TestWaitForRateLimit_ContextCancellation verifies that rate limiter respects context cancellation
func TestWaitForRateLimit_ContextCancellation(t *testing.T) {
	// Save current limiter and restore after test
	savedLimiter := limiter
	defer func() { limiter = savedLimiter }()

	// Initialize with rate of 2 req/sec (500ms between requests)
	InitRateLimiter(2)
	if limiter == nil {
		t.Fatal("Expected limiter to be initialized")
	}

	// Consume the burst token with the first Wait call
	bgCtx := context.Background()
	if err := limiter.Wait(bgCtx); err != nil {
		t.Fatal("Expected first wait to succeed")
	}

	// Now the next Wait should need to wait ~500ms for the next token
	// Create a context with enough deadline for the wait to begin,
	// but cancel it manually partway through
	ctx, cancel := context.WithTimeout(context.Background(), 800*time.Millisecond)

	// Cancel the context after 100ms (while the 500ms wait is in progress)
	go func() {
		time.Sleep(100 * time.Millisecond)
		cancel()
	}()

	start := time.Now()
	err := WaitForRateLimit(ctx)
	elapsed := time.Since(start)

	if err == nil {
		t.Error("Expected error from WaitForRateLimit when context is cancelled, but got nil")
	}

	// Should return after ~100ms when we cancelled (allow some variance)
	if elapsed < 80*time.Millisecond || elapsed > 250*time.Millisecond {
		t.Errorf("Expected cancellation after ~100ms, but took %v (error: %v)", elapsed, err)
	}
}

// TestRateLimitTiming verifies that rate limiting actually throttles requests
func TestRateLimitTiming(t *testing.T) {
	requestsPerSecond := 5
	InitRateLimiter(requestsPerSecond)
	if limiter == nil {
		t.Fatal("Expected limiter to be initialized")
	}

	ctx := context.Background()
	requestCount := 3

	start := time.Now()
	for i := 0; i < requestCount; i++ {
		err := WaitForRateLimit(ctx)
		if err != nil {
			t.Fatalf("Unexpected error from WaitForRateLimit: %v", err)
		}
	}
	elapsed := time.Since(start)

	// With a burst of 1 and rate of 5 req/sec:
	// - First request: immediate (uses burst token)
	// - Second request: waits 200ms (1/5 second)
	// - Third request: waits another 200ms
	// Total expected: ~400ms
	expectedMinDuration := 400 * time.Millisecond

	// Allow some tolerance for timing variance (system scheduling, etc.)
	// but it should be close to expected
	if elapsed < expectedMinDuration {
		t.Errorf("Rate limiting too fast: expected at least %v for %d requests at %d req/sec, got %v",
			expectedMinDuration, requestCount, requestsPerSecond, elapsed)
	}

	// Upper bound: should not take more than 2x the expected time
	// (allows for some variance but catches major issues)
	maxDuration := expectedMinDuration * 2
	if elapsed > maxDuration {
		t.Errorf("Rate limiting too slow: expected less than %v, got %v", maxDuration, elapsed)
	}
}

// TestWaitForRateLimit_MultipleCallsSequential verifies sequential calls are properly throttled
func TestWaitForRateLimit_MultipleCallsSequential(t *testing.T) {
	InitRateLimiter(10) // 10 requests per second = 100ms between requests
	if limiter == nil {
		t.Fatal("Expected limiter to be initialized")
	}

	ctx := context.Background()

	// Make 5 sequential calls and track timing
	callCount := 5
	start := time.Now()

	for i := 0; i < callCount; i++ {
		err := WaitForRateLimit(ctx)
		if err != nil {
			t.Fatalf("Call %d failed: %v", i, err)
		}
	}

	elapsed := time.Since(start)

	// With burst of 1 and 10 req/sec (100ms per request):
	// First call immediate, then 4 * 100ms = 400ms minimum
	minExpected := 400 * time.Millisecond

	if elapsed < minExpected {
		t.Errorf("Expected at least %v for %d sequential calls, got %v", minExpected, callCount, elapsed)
	}
}
