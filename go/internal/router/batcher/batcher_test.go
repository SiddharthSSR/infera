package batcher

import (
	"sync"
	"testing"
	"time"

	"github.com/infera/infera/go/pkg/types"
)

func makeRequest(model string, content string, stream bool, priority types.Priority) *types.RoutedRequest {
	return &types.RoutedRequest{
		Request: &types.InferenceRequest{
			RequestID: "req-" + content,
			ModelID:   model,
			Messages:  []types.Message{{Role: types.RoleUser, Content: content}},
			Stream:    stream,
			Priority:  priority,
		},
		WorkerID: "worker-1",
		RoutedAt: time.Now(),
	}
}

func TestShouldBatch(t *testing.T) {
	m := NewManager(DefaultConfig())
	defer m.Stop()

	t.Run("normal request should batch", func(t *testing.T) {
		req := &types.InferenceRequest{ModelID: "model-a", Priority: types.PriorityNormal}
		if !m.ShouldBatch(req) {
			t.Error("expected normal request to be eligible for batching")
		}
	})

	t.Run("streaming request should not batch", func(t *testing.T) {
		req := &types.InferenceRequest{Stream: true}
		if m.ShouldBatch(req) {
			t.Error("streaming requests should not batch")
		}
	})

	t.Run("high priority should not batch", func(t *testing.T) {
		req := &types.InferenceRequest{Priority: types.PriorityHigh}
		if m.ShouldBatch(req) {
			t.Error("high priority requests should not batch")
		}
	})

	t.Run("disabled manager should not batch", func(t *testing.T) {
		disabled := NewManager(Config{Enabled: false})
		defer disabled.Stop()
		req := &types.InferenceRequest{Priority: types.PriorityNormal}
		if disabled.ShouldBatch(req) {
			t.Error("disabled manager should not batch")
		}
	})
}

func TestEnqueueAndQueueDepth(t *testing.T) {
	cfg := DefaultConfig()
	cfg.MaxBatchSize = 100 // prevent auto-seal
	cfg.MaxWaitMS = 5000   // prevent timeout-based seal in this test
	m := NewManager(cfg)
	defer m.Stop()

	m.Enqueue(makeRequest("llama", "hello", false, types.PriorityNormal))
	m.Enqueue(makeRequest("llama", "world", false, types.PriorityNormal))
	m.Enqueue(makeRequest("mistral", "hi", false, types.PriorityNormal))

	total := m.GetQueueDepth()
	if total != 3 {
		t.Errorf("expected total queue depth 3, got %d", total)
	}

	llama := m.GetQueueDepthForModel("llama")
	if llama != 2 {
		t.Errorf("expected llama queue depth 2, got %d", llama)
	}

	mistral := m.GetQueueDepthForModel("mistral")
	if mistral != 1 {
		t.Errorf("expected mistral queue depth 1, got %d", mistral)
	}

	unknown := m.GetQueueDepthForModel("unknown")
	if unknown != 0 {
		t.Errorf("expected unknown queue depth 0, got %d", unknown)
	}
}

func TestBatchSealOnMaxSize(t *testing.T) {
	cfg := Config{
		MaxBatchSize:      3,
		MaxWaitMS:         5000, // long timeout, so only size triggers
		MaxTokensPerBatch: 99999,
		Enabled:           true,
	}

	var mu sync.Mutex
	var dispatched []*types.BatchContext
	done := make(chan struct{}, 1)

	m := NewManager(cfg)
	m.OnBatchReady(func(batch *types.BatchContext) {
		mu.Lock()
		dispatched = append(dispatched, batch)
		mu.Unlock()
		select {
		case done <- struct{}{}:
		default:
		}
	})
	defer m.Stop()

	// Enqueue 3 requests — should trigger batch seal
	m.Enqueue(makeRequest("model", "a", false, types.PriorityNormal))
	m.Enqueue(makeRequest("model", "b", false, types.PriorityNormal))
	m.Enqueue(makeRequest("model", "c", false, types.PriorityNormal))

	select {
	case <-done:
	case <-time.After(2 * time.Second):
		t.Fatal("timed out waiting for size-based batch dispatch")
	}

	mu.Lock()
	defer mu.Unlock()
	if len(dispatched) != 1 {
		t.Fatalf("expected 1 dispatched batch, got %d", len(dispatched))
	}
	if dispatched[0].Size() != 3 {
		t.Errorf("expected batch size 3, got %d", dispatched[0].Size())
	}
	if !dispatched[0].IsSealed() {
		t.Error("expected batch to be sealed")
	}
}

func TestBatchSealOnTimeout(t *testing.T) {
	cfg := Config{
		MaxBatchSize:      100,
		MaxWaitMS:         30, // very short timeout
		MaxTokensPerBatch: 99999,
		Enabled:           true,
	}

	var mu sync.Mutex
	var dispatched []*types.BatchContext
	done := make(chan struct{}, 1)

	m := NewManager(cfg)
	m.OnBatchReady(func(batch *types.BatchContext) {
		mu.Lock()
		dispatched = append(dispatched, batch)
		mu.Unlock()
		select {
		case done <- struct{}{}:
		default:
		}
	})
	defer m.Stop()

	m.Enqueue(makeRequest("model", "hello", false, types.PriorityNormal))

	select {
	case <-done:
	case <-time.After(2 * time.Second):
		t.Fatal("timed out waiting for timeout-based batch dispatch")
	}

	mu.Lock()
	defer mu.Unlock()
	if len(dispatched) != 1 {
		t.Fatalf("expected 1 dispatched batch on timeout, got %d", len(dispatched))
	}
}

func TestQueueFlush(t *testing.T) {
	cfg := DefaultConfig()
	cfg.MaxBatchSize = 100
	cfg.MaxWaitMS = 10000

	q := NewQueue("model", cfg, nil)
	q.Enqueue(&types.RoutedRequest{
		Request: &types.InferenceRequest{
			RequestID: "req-1",
			ModelID:   "model",
			Messages:  []types.Message{{Content: "test"}},
		},
	})

	batch := q.Flush()
	if batch == nil {
		t.Fatal("expected batch from flush")
	}
	if batch.Size() != 1 {
		t.Errorf("expected 1 request in flushed batch, got %d", batch.Size())
	}

	// Second flush should return nil
	batch = q.Flush()
	if batch != nil {
		t.Error("expected nil from empty flush")
	}
}

func TestNewBatchUsesShortFirstWaitWindow(t *testing.T) {
	cfg := DefaultConfig()
	cfg.MaxWaitMS = 50

	q := NewQueue("model", cfg, nil)
	batch := q.newBatch()

	if batch.MaxWaitMS != fastFirstBatchWaitMS {
		t.Fatalf("expected first-batch wait %dms, got %dms", fastFirstBatchWaitMS, batch.MaxWaitMS)
	}
}

func TestDefaultConfig(t *testing.T) {
	cfg := DefaultConfig()
	if cfg.MaxBatchSize != 8 {
		t.Errorf("expected max batch size 8, got %d", cfg.MaxBatchSize)
	}
	if cfg.MaxWaitMS != 50 {
		t.Errorf("expected max wait 50, got %d", cfg.MaxWaitMS)
	}
	if cfg.MaxTokensPerBatch != 4096 {
		t.Errorf("expected max tokens 4096, got %d", cfg.MaxTokensPerBatch)
	}
	if !cfg.Enabled {
		t.Error("expected enabled by default")
	}
}
