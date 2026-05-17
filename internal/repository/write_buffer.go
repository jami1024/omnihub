package repository

import (
	"context"
	"log/slog"
	"sync"
	"time"
)

// MessageRequestSink is the minimal interface WriteBuffer needs from
// the repository: batched persistence. Defined here so tests can
// substitute an in-memory implementation without spinning up Postgres.
type MessageRequestSink interface {
	InsertBatch(ctx context.Context, batch []MessageRequest) error
}

// WriteBufferConfig tunes flush behaviour. Zero values fall back to
// production-sane defaults.
type WriteBufferConfig struct {
	// FlushInterval is the maximum time a record waits before being
	// written. Default 250 ms.
	FlushInterval time.Duration
	// BatchSize triggers an immediate flush when reached. Default 200.
	BatchSize int
	// MaxPending caps the queue. When exceeded the oldest record is
	// dropped to prevent unbounded memory growth under outage. Default 5000.
	MaxPending int
	// WriteTimeout caps how long a single flush may take. Default 10 s.
	WriteTimeout time.Duration
}

func (c WriteBufferConfig) withDefaults() WriteBufferConfig {
	if c.FlushInterval <= 0 {
		c.FlushInterval = 250 * time.Millisecond
	}
	if c.BatchSize <= 0 {
		c.BatchSize = 200
	}
	if c.MaxPending <= 0 {
		c.MaxPending = 5000
	}
	if c.WriteTimeout <= 0 {
		c.WriteTimeout = 10 * time.Second
	}
	return c
}

// WriteBuffer collects MessageRequest records and flushes them to the
// sink in batches. It is safe for concurrent producers; one flusher
// runs at a time.
//
// The design follows the model used by claude-code-hub's
// MessageRequestWriteBuffer (250 ms / 200 rows / serialised flushes,
// failure puts the batch back on the queue, drop-oldest under
// overflow). It is intentionally insert-only — patches in-place are
// not modelled because OmniHub builds the full record once at the
// end of each request.
type WriteBuffer struct {
	sink   MessageRequestSink
	config WriteBufferConfig

	mu      sync.Mutex
	pending []MessageRequest

	flushMu  sync.Mutex // serialises calls into the sink
	timerMu  sync.Mutex
	timer    *time.Timer

	stopOnce sync.Once
	stopping bool
}

// NewWriteBuffer constructs a WriteBuffer that writes into sink.
// The buffer is ready to accept Enqueue calls immediately; no goroutine
// is spawned (timers fire on demand).
func NewWriteBuffer(sink MessageRequestSink, cfg WriteBufferConfig) *WriteBuffer {
	return &WriteBuffer{
		sink:   sink,
		config: cfg.withDefaults(),
	}
}

// Enqueue queues rec for the next flush. Returns immediately without
// any I/O. After Stop has been called Enqueue is a no-op.
//
// When the queue size hits BatchSize the buffer triggers an
// out-of-band flush in a new goroutine so the producer does not block
// on a slow DB.
func (b *WriteBuffer) Enqueue(rec MessageRequest) {
	b.mu.Lock()
	if b.stopping {
		b.mu.Unlock()
		return
	}

	if len(b.pending) >= b.config.MaxPending {
		// Drop the oldest record so memory cannot grow without bound
		// during an outage. We log at warn so operators see it.
		dropped := b.pending[0]
		b.pending = b.pending[1:]
		slog.Warn("WriteBuffer overflow, dropping oldest record",
			"max_pending", b.config.MaxPending,
			"dropped_model", dropped.Model,
			"dropped_age_ms", time.Since(dropped.CreatedAt).Milliseconds(),
		)
	}

	b.pending = append(b.pending, rec)
	size := len(b.pending)
	b.mu.Unlock()

	if size >= b.config.BatchSize {
		go b.Flush(context.Background())
		return
	}
	b.ensureTimer()
}

func (b *WriteBuffer) ensureTimer() {
	b.timerMu.Lock()
	defer b.timerMu.Unlock()
	if b.timer != nil {
		return
	}
	b.timer = time.AfterFunc(b.config.FlushInterval, func() {
		b.timerMu.Lock()
		b.timer = nil
		b.timerMu.Unlock()
		b.Flush(context.Background())
	})
}

func (b *WriteBuffer) clearTimer() {
	b.timerMu.Lock()
	defer b.timerMu.Unlock()
	if b.timer != nil {
		b.timer.Stop()
		b.timer = nil
	}
}

// Flush drains the queue to the sink. It is safe to call concurrently;
// flushes are serialised inside the buffer. The optional ctx caps how
// long this particular call may wait for the DB.
func (b *WriteBuffer) Flush(ctx context.Context) {
	b.flushMu.Lock()
	defer b.flushMu.Unlock()
	b.clearTimer()

	for {
		b.mu.Lock()
		if len(b.pending) == 0 {
			b.mu.Unlock()
			return
		}
		take := b.config.BatchSize
		if take > len(b.pending) {
			take = len(b.pending)
		}
		batch := make([]MessageRequest, take)
		copy(batch, b.pending[:take])
		b.pending = b.pending[take:]
		b.mu.Unlock()

		writeCtx, cancel := context.WithTimeout(ctx, b.config.WriteTimeout)
		err := b.sink.InsertBatch(writeCtx, batch)
		cancel()

		if err != nil {
			slog.Error("WriteBuffer flush failed; requeueing batch",
				"batch_size", len(batch),
				"err", err.Error(),
			)
			// Put the failed batch back at the head of the queue so
			// later flushes retry it.
			b.mu.Lock()
			b.pending = append(batch, b.pending...)
			b.mu.Unlock()
			// Avoid hot-looping on a sick DB; let the next tick try.
			b.ensureTimer()
			return
		}
		slog.Debug("WriteBuffer flushed", "batch_size", len(batch))
	}
}

// Stop signals the buffer to refuse new enqueues, cancels any
// outstanding timer, and drains the queue in up to two flush attempts.
// The first flush handles steady-state pending records; the second
// catches anything requeued by a failure during the first flush.
func (b *WriteBuffer) Stop(ctx context.Context) {
	b.stopOnce.Do(func() {
		b.mu.Lock()
		b.stopping = true
		b.mu.Unlock()
		b.clearTimer()

		b.Flush(ctx)

		b.mu.Lock()
		needsRetry := len(b.pending) > 0
		b.mu.Unlock()
		if needsRetry {
			b.Flush(ctx)
		}
	})
}
