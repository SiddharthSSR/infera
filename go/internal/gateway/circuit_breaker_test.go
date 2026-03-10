package gateway

import (
	"testing"
	"time"
)

func TestCircuitBreakerStartsClosed(t *testing.T) {
	cb := NewCircuitBreaker()
	if !cb.Allow() {
		t.Error("new circuit breaker should allow requests")
	}
	if cb.State() != "closed" {
		t.Errorf("expected closed, got %s", cb.State())
	}
}

func TestCircuitBreakerOpensAfterThreshold(t *testing.T) {
	cb := NewCircuitBreaker()

	for i := 0; i < 5; i++ {
		cb.RecordFailure()
	}

	if cb.Allow() {
		t.Error("should not allow requests when circuit is open")
	}
	if cb.State() != "open" {
		t.Errorf("expected open, got %s", cb.State())
	}
}

func TestCircuitBreakerStaysClosedBelowThreshold(t *testing.T) {
	cb := NewCircuitBreaker()

	for i := 0; i < 4; i++ {
		cb.RecordFailure()
	}

	if !cb.Allow() {
		t.Error("should still allow requests below threshold")
	}
	if cb.State() != "closed" {
		t.Errorf("expected closed, got %s", cb.State())
	}
}

func TestCircuitBreakerResetsOnSuccess(t *testing.T) {
	cb := NewCircuitBreaker()

	// Get close to threshold
	for i := 0; i < 4; i++ {
		cb.RecordFailure()
	}

	// Success resets counter
	cb.RecordSuccess()

	// Now 5 more failures needed
	for i := 0; i < 4; i++ {
		cb.RecordFailure()
	}
	if !cb.Allow() {
		t.Error("should still allow after success reset")
	}
}

func TestCircuitBreakerHalfOpen(t *testing.T) {
	cb := &CircuitBreaker{
		failureThreshold: 2,
		resetTimeout:     10 * time.Millisecond,
	}

	cb.RecordFailure()
	cb.RecordFailure()

	if cb.Allow() {
		t.Error("should be open immediately after tripping")
	}

	// Wait for reset timeout
	time.Sleep(20 * time.Millisecond)

	// Should transition to half-open and allow one request
	if !cb.Allow() {
		t.Error("should allow after reset timeout (half-open)")
	}
	if cb.State() != "half_open" {
		t.Errorf("expected half_open, got %s", cb.State())
	}
}

func TestCircuitBreakerHalfOpenSuccess(t *testing.T) {
	cb := &CircuitBreaker{
		failureThreshold: 2,
		resetTimeout:     10 * time.Millisecond,
	}

	cb.RecordFailure()
	cb.RecordFailure()
	time.Sleep(20 * time.Millisecond)

	cb.Allow() // transitions to half-open
	cb.RecordSuccess()

	if cb.State() != "closed" {
		t.Errorf("expected closed after half-open success, got %s", cb.State())
	}
}

func TestCircuitBreakerHalfOpenFailure(t *testing.T) {
	cb := &CircuitBreaker{
		failureThreshold: 2,
		resetTimeout:     10 * time.Millisecond,
	}

	cb.RecordFailure()
	cb.RecordFailure()
	time.Sleep(20 * time.Millisecond)

	cb.Allow() // transitions to half-open
	cb.RecordFailure()

	if cb.State() != "open" {
		t.Errorf("expected open after half-open failure, got %s", cb.State())
	}
}

func TestCircuitBreakerReset(t *testing.T) {
	cb := NewCircuitBreaker()

	for i := 0; i < 5; i++ {
		cb.RecordFailure()
	}
	if cb.Allow() {
		t.Error("should be open")
	}

	cb.Reset()
	if !cb.Allow() {
		t.Error("should allow after reset")
	}
	if cb.State() != "closed" {
		t.Errorf("expected closed after reset, got %s", cb.State())
	}
}
