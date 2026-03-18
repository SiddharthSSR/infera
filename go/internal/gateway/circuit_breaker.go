package gateway

import (
	"errors"
	"math/rand"
	"sync"
	"time"
)

// Circuit breaker states.
const (
	circuitClosed   = iota // Normal operation — requests flow through
	circuitOpen            // Tripped — all requests fail fast
	circuitHalfOpen        // Testing — allow one probe request
)

// ErrCircuitOpen is returned when the circuit breaker is open.
var ErrCircuitOpen = errors.New("circuit breaker is open: worker unavailable")

// CircuitBreaker implements a simple circuit breaker per worker.
//
// Transitions:
//
//	CLOSED → OPEN: after failureThreshold consecutive failures
//	OPEN → HALF-OPEN: after resetTimeout (+jitter) elapses
//	HALF-OPEN → CLOSED: on success
//	HALF-OPEN → OPEN: on failure
//
// Jitter (±20% of resetTimeout) prevents a thundering herd when multiple
// workers trip at the same time and would otherwise all probe simultaneously.
type CircuitBreaker struct {
	mu                    sync.Mutex
	state                 int
	failures              int
	failureThreshold      int
	resetTimeout          time.Duration
	nextResetAt           time.Time
	halfOpenProbeInFlight bool
}

// NewCircuitBreaker creates a circuit breaker with sensible defaults.
func NewCircuitBreaker() *CircuitBreaker {
	return &CircuitBreaker{
		failureThreshold: 5,
		resetTimeout:     10 * time.Second,
	}
}

// jitteredResetTimeout returns resetTimeout ±20% to stagger recovery probes.
func (cb *CircuitBreaker) jitteredResetTimeout() time.Duration {
	jitter := float64(cb.resetTimeout) * 0.2
	return cb.resetTimeout + time.Duration((rand.Float64()*2-1)*jitter)
}

// Allow checks whether a request should be allowed through.
func (cb *CircuitBreaker) Allow() bool {
	cb.mu.Lock()
	defer cb.mu.Unlock()

	switch cb.state {
	case circuitClosed:
		return true
	case circuitOpen:
		if time.Now().After(cb.nextResetAt) {
			cb.state = circuitHalfOpen
			cb.halfOpenProbeInFlight = true
			return true
		}
		return false
	case circuitHalfOpen:
		if cb.halfOpenProbeInFlight {
			return false
		}
		cb.halfOpenProbeInFlight = true
		return true
	}
	return false
}

// RecordSuccess records a successful request.
func (cb *CircuitBreaker) RecordSuccess() {
	cb.mu.Lock()
	defer cb.mu.Unlock()

	cb.failures = 0
	cb.state = circuitClosed
	cb.halfOpenProbeInFlight = false
}

// RecordFailure records a failed request and potentially opens the circuit.
func (cb *CircuitBreaker) RecordFailure() {
	cb.mu.Lock()
	defer cb.mu.Unlock()

	cb.failures++
	cb.halfOpenProbeInFlight = false

	if cb.failures >= cb.failureThreshold {
		cb.state = circuitOpen
		cb.nextResetAt = time.Now().Add(cb.jitteredResetTimeout())
	}
}

// State returns the current state as a string (for logging/stats).
func (cb *CircuitBreaker) State() string {
	cb.mu.Lock()
	defer cb.mu.Unlock()

	switch cb.state {
	case circuitOpen:
		return "open"
	case circuitHalfOpen:
		return "half_open"
	default:
		return "closed"
	}
}

// Reset returns the breaker to closed state.
func (cb *CircuitBreaker) Reset() {
	cb.mu.Lock()
	defer cb.mu.Unlock()

	cb.state = circuitClosed
	cb.failures = 0
	cb.halfOpenProbeInFlight = false
}
