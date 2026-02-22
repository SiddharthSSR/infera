package batcher

import (
	"context"
	"sync"
	"time"

	"github.com/google/uuid"
	"github.com/infera/infera/go/pkg/types"
)

// Config configures batching behavior.
type Config struct {
	MaxBatchSize      int
	MaxWaitMS         int
	MaxTokensPerBatch int
	Enabled           bool
}

// DefaultConfig returns sensible defaults.
func DefaultConfig() Config {
	return Config{
		MaxBatchSize:      8,
		MaxWaitMS:         50,
		MaxTokensPerBatch: 4096,
		Enabled:           true,
	}
}

// BatchReadyCallback is called when a batch is ready for dispatch.
type BatchReadyCallback func(batch *types.BatchContext)

// Manager manages batching queues per model.
type Manager struct {
	queues   map[string]*Queue
	config   Config
	callback BatchReadyCallback
	mu       sync.RWMutex
	ctx      context.Context
	cancel   context.CancelFunc
}

// NewManager creates a new batch manager.
func NewManager(config Config) *Manager {
	ctx, cancel := context.WithCancel(context.Background())
	return &Manager{
		queues: make(map[string]*Queue),
		config: config,
		ctx:    ctx,
		cancel: cancel,
	}
}

// OnBatchReady sets the callback for when batches are ready.
func (m *Manager) OnBatchReady(callback BatchReadyCallback) {
	m.callback = callback
}

// ShouldBatch determines if a request should be batched.
func (m *Manager) ShouldBatch(request *types.InferenceRequest) bool {
	if !m.config.Enabled || request.Stream || request.Priority == types.PriorityHigh {
		return false
	}
	return true
}

// Enqueue adds a request to the appropriate batch queue.
func (m *Manager) Enqueue(request *types.RoutedRequest) {
	queue := m.getOrCreateQueue(request.Request.ModelID)
	queue.Enqueue(request)
}

// GetQueueDepth returns the total queue depth across all models.
func (m *Manager) GetQueueDepth() int {
	m.mu.RLock()
	defer m.mu.RUnlock()

	total := 0
	for _, q := range m.queues {
		total += q.Size()
	}
	return total
}

// GetQueueDepthForModel returns the queue depth for a specific model.
func (m *Manager) GetQueueDepthForModel(modelID string) int {
	m.mu.RLock()
	queue, exists := m.queues[modelID]
	m.mu.RUnlock()

	if !exists {
		return 0
	}
	return queue.Size()
}

// Stop shuts down the batch manager.
func (m *Manager) Stop() {
	m.cancel()
	m.mu.Lock()
	defer m.mu.Unlock()
	for _, q := range m.queues {
		q.Stop()
	}
}

func (m *Manager) getOrCreateQueue(modelID string) *Queue {
	m.mu.RLock()
	queue, exists := m.queues[modelID]
	m.mu.RUnlock()

	if exists {
		return queue
	}

	m.mu.Lock()
	defer m.mu.Unlock()

	if queue, exists = m.queues[modelID]; exists {
		return queue
	}

	queue = NewQueue(modelID, m.config, m.callback)
	queue.Start(m.ctx)
	m.queues[modelID] = queue
	return queue
}

// Queue manages batching for a single model.
type Queue struct {
	modelID      string
	config       Config
	callback     BatchReadyCallback
	pending      []*types.RoutedRequest
	currentBatch *types.BatchContext
	mu           sync.Mutex
	ctx          context.Context
	cancel       context.CancelFunc
}

// NewQueue creates a new batch queue.
func NewQueue(modelID string, config Config, callback BatchReadyCallback) *Queue {
	return &Queue{
		modelID:  modelID,
		config:   config,
		callback: callback,
		pending:  make([]*types.RoutedRequest, 0),
	}
}

// Start begins the batch formation goroutine.
func (q *Queue) Start(parentCtx context.Context) {
	q.ctx, q.cancel = context.WithCancel(parentCtx)
	go q.batchFormationLoop()
}

// Stop stops the batch formation goroutine.
func (q *Queue) Stop() {
	if q.cancel != nil {
		q.cancel()
	}
}

// Enqueue adds a request to the queue.
func (q *Queue) Enqueue(request *types.RoutedRequest) {
	q.mu.Lock()
	defer q.mu.Unlock()

	if q.currentBatch == nil {
		q.currentBatch = q.newBatch()
	}

	request.BatchID = q.currentBatch.BatchID
	q.pending = append(q.pending, request)

	if q.isBatchReady() {
		q.sealAndDispatch()
	}
}

// Size returns the number of pending requests.
func (q *Queue) Size() int {
	q.mu.Lock()
	defer q.mu.Unlock()
	return len(q.pending)
}

func (q *Queue) batchFormationLoop() {
	ticker := time.NewTicker(time.Duration(q.config.MaxWaitMS/2) * time.Millisecond)
	defer ticker.Stop()

	for {
		select {
		case <-q.ctx.Done():
			q.Flush()
			return
		case <-ticker.C:
			q.checkTimeout()
		}
	}
}

func (q *Queue) checkTimeout() {
	q.mu.Lock()
	defer q.mu.Unlock()

	if q.currentBatch != nil && q.currentBatch.IsExpired() && len(q.pending) > 0 {
		q.sealAndDispatch()
	}
}

func (q *Queue) isBatchReady() bool {
	if len(q.pending) >= q.config.MaxBatchSize {
		return true
	}

	totalTokens := 0
	for _, req := range q.pending {
		totalTokens += req.Request.TokenEstimate()
	}
	return totalTokens >= q.config.MaxTokensPerBatch
}

func (q *Queue) sealAndDispatch() *types.BatchContext {
	if len(q.pending) == 0 {
		return nil
	}

	batch := q.currentBatch
	batch.Requests = q.pending
	batch.Seal()

	q.pending = make([]*types.RoutedRequest, 0)
	q.currentBatch = nil

	if q.callback != nil {
		go q.callback(batch)
	}

	return batch
}

func (q *Queue) Flush() *types.BatchContext {
	q.mu.Lock()
	defer q.mu.Unlock()
	if len(q.pending) == 0 {
		return nil
	}
	return q.sealAndDispatch()
}

func (q *Queue) newBatch() *types.BatchContext {
	return &types.BatchContext{
		BatchID:   uuid.New().String(),
		Requests:  make([]*types.RoutedRequest, 0),
		ModelID:   q.modelID,
		CreatedAt: time.Now(),
		MaxSize:   q.config.MaxBatchSize,
		MaxWaitMS: q.config.MaxWaitMS,
	}
}
