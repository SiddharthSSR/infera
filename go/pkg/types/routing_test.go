package types

import (
	"testing"
	"time"
)

func TestRoutedRequest(t *testing.T) {
	t.Run("IsBatched", func(t *testing.T) {
		r := &RoutedRequest{}
		if r.IsBatched() {
			t.Error("should not be batched without BatchID")
		}
		r.BatchID = "batch-1"
		if !r.IsBatched() {
			t.Error("should be batched with BatchID")
		}
	})

	t.Run("IsRetriable", func(t *testing.T) {
		r := &RoutedRequest{Attempt: 1, MaxAttempts: 3}
		if !r.IsRetriable() {
			t.Error("should be retriable")
		}
		r.Attempt = 3
		if r.IsRetriable() {
			t.Error("should not be retriable at max attempts")
		}
	})

	t.Run("IsExpired", func(t *testing.T) {
		r := &RoutedRequest{Deadline: time.Now().Add(time.Hour)}
		if r.IsExpired() {
			t.Error("should not be expired")
		}
		r.Deadline = time.Now().Add(-time.Hour)
		if !r.IsExpired() {
			t.Error("should be expired")
		}
	})

	t.Run("WithRetry", func(t *testing.T) {
		r := &RoutedRequest{
			Request:     &InferenceRequest{RequestID: "req-1"},
			Attempt:     1,
			MaxAttempts: 3,
			Deadline:    time.Now().Add(time.Hour),
		}
		retry := r.WithRetry()
		if retry.Attempt != 2 {
			t.Errorf("expected attempt 2, got %d", retry.Attempt)
		}
		if retry.Request.RequestID != "req-1" {
			t.Error("retry should keep same request")
		}
	})
}

func TestBatchContext(t *testing.T) {
	b := &BatchContext{
		BatchID:   "batch-1",
		MaxSize:   3,
		MaxWaitMS: 50,
		CreatedAt: time.Now(),
		Requests:  make([]*RoutedRequest, 0),
	}

	t.Run("Add", func(t *testing.T) {
		req := &RoutedRequest{Request: &InferenceRequest{RequestID: "r1"}}
		if !b.Add(req) {
			t.Error("should be able to add request")
		}
		if b.Size() != 1 {
			t.Errorf("expected size 1, got %d", b.Size())
		}
	})

	t.Run("IsFull", func(t *testing.T) {
		b.Add(&RoutedRequest{Request: &InferenceRequest{RequestID: "r2"}})
		b.Add(&RoutedRequest{Request: &InferenceRequest{RequestID: "r3"}})
		if !b.IsFull() {
			t.Error("batch should be full at max size")
		}

		// Adding to full batch should fail
		if b.Add(&RoutedRequest{Request: &InferenceRequest{RequestID: "r4"}}) {
			t.Error("should not add to full batch")
		}
	})

	t.Run("Seal", func(t *testing.T) {
		if b.IsSealed() {
			t.Error("should not be sealed yet")
		}
		b.Seal()
		if !b.IsSealed() {
			t.Error("should be sealed after Seal()")
		}

		// Adding to sealed batch should fail
		if b.Add(&RoutedRequest{Request: &InferenceRequest{RequestID: "r5"}}) {
			t.Error("should not add to sealed batch")
		}
	})

	t.Run("IsExpired", func(t *testing.T) {
		fresh := &BatchContext{CreatedAt: time.Now(), MaxWaitMS: 1000}
		if fresh.IsExpired() {
			t.Error("fresh batch should not be expired")
		}

		old := &BatchContext{CreatedAt: time.Now().Add(-2 * time.Second), MaxWaitMS: 50}
		if !old.IsExpired() {
			t.Error("old batch should be expired")
		}
	})
}

func TestRequestTracker(t *testing.T) {
	tracker := &RequestTracker{RequestID: "req-1", State: RequestStateReceived}

	tracker.Transition(RequestStateQueued, "validated")
	if tracker.State != RequestStateQueued {
		t.Errorf("expected queued, got %s", tracker.State)
	}

	tracker.Transition(RequestStateCompleted, "done")
	if tracker.State != RequestStateCompleted {
		t.Errorf("expected completed, got %s", tracker.State)
	}
}

func TestInferenceRequest(t *testing.T) {
	t.Run("NewInferenceRequest", func(t *testing.T) {
		req := NewInferenceRequest("llama-8b", []Message{{Role: RoleUser, Content: "hello"}})
		if req.RequestID == "" {
			t.Error("expected generated request ID")
		}
		if req.ModelID != "llama-8b" {
			t.Errorf("expected llama-8b, got %s", req.ModelID)
		}
		if req.Priority != PriorityNormal {
			t.Errorf("expected normal priority, got %d", req.Priority)
		}
	})

	t.Run("TokenEstimate", func(t *testing.T) {
		req := &InferenceRequest{
			Messages: []Message{
				{Content: "hello world"},         // 11 chars = ~2 tokens
				{Content: "this is a test input"}, // 20 chars = ~5 tokens
			},
		}
		estimate := req.TokenEstimate()
		if estimate < 5 || estimate > 10 {
			t.Errorf("unexpected token estimate: %d", estimate)
		}
	})

	t.Run("DefaultInferenceParameters", func(t *testing.T) {
		p := DefaultInferenceParameters()
		if p.MaxTokens != 256 {
			t.Errorf("expected 256, got %d", p.MaxTokens)
		}
		if p.Temperature != 1.0 {
			t.Errorf("expected 1.0, got %f", p.Temperature)
		}
		if p.TopP != 1.0 {
			t.Errorf("expected 1.0, got %f", p.TopP)
		}
	})
}

func TestTokenChunk(t *testing.T) {
	t.Run("IsFinal with no finish reason", func(t *testing.T) {
		c := &TokenChunk{Delta: "hello"}
		if c.IsFinal() {
			t.Error("should not be final without finish reason")
		}
	})

	t.Run("IsFinal with finish reason", func(t *testing.T) {
		reason := FinishReasonStop
		c := &TokenChunk{FinishReason: &reason}
		if !c.IsFinal() {
			t.Error("should be final with finish reason")
		}
	})
}

func TestInferaError(t *testing.T) {
	err := NewInferaError(ErrorCodeModelNotFound, "model not found")
	if err.Error() != "model not found" {
		t.Errorf("unexpected error message: %s", err.Error())
	}

	err.WithRequestID("req-123")
	if err.RequestID != "req-123" {
		t.Errorf("expected req-123, got %s", err.RequestID)
	}
}
