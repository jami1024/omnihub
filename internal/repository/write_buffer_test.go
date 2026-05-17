package repository_test

import (
	"context"
	"errors"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/jami1024/omnihub/internal/repository"
)

// fakeSink records every batch it receives and can be programmed to
// fail the next N calls.
type fakeSink struct {
	mu        sync.Mutex
	batches   [][]repository.MessageRequest
	failures  atomic.Int32
	totalRows atomic.Int64
}

func (f *fakeSink) InsertBatch(_ context.Context, batch []repository.MessageRequest) error {
	if f.failures.Add(-1) >= 0 {
		return errors.New("simulated DB failure")
	}
	// failures dropped below 0; restore to 0 so subsequent calls succeed.
	f.failures.Store(0)
	f.mu.Lock()
	defer f.mu.Unlock()
	cp := make([]repository.MessageRequest, len(batch))
	copy(cp, batch)
	f.batches = append(f.batches, cp)
	f.totalRows.Add(int64(len(batch)))
	return nil
}

func (f *fakeSink) Snapshot() [][]repository.MessageRequest {
	f.mu.Lock()
	defer f.mu.Unlock()
	out := make([][]repository.MessageRequest, len(f.batches))
	copy(out, f.batches)
	return out
}

func sampleRecord(i int) repository.MessageRequest {
	model := "claude-haiku-4-5"
	return repository.MessageRequest{
		CreatedAt:    time.Now(),
		Method:       "POST",
		Path:         "/v1/messages",
		Model:        model,
		ProviderName: "anthropic",
		AccountName:  "default",
		InputTokens:  int64(i),
		OutputTokens: int64(i * 2),
	}
}

func TestWriteBufferTimerFlushesUnderBatchSize(t *testing.T) {
	sink := &fakeSink{}
	b := repository.NewWriteBuffer(sink, repository.WriteBufferConfig{
		FlushInterval: 30 * time.Millisecond,
		BatchSize:     100, // intentionally never hit
	})

	for i := 0; i < 3; i++ {
		b.Enqueue(sampleRecord(i))
	}

	// Wait long enough for the timer to fire.
	time.Sleep(120 * time.Millisecond)

	if got := sink.totalRows.Load(); got != 3 {
		t.Errorf("expected 3 rows written after timer flush, got %d", got)
	}
}

func TestWriteBufferBatchSizeTriggersImmediateFlush(t *testing.T) {
	sink := &fakeSink{}
	b := repository.NewWriteBuffer(sink, repository.WriteBufferConfig{
		FlushInterval: 10 * time.Second, // never fires within test
		BatchSize:     5,
	})

	for i := 0; i < 5; i++ {
		b.Enqueue(sampleRecord(i))
	}

	// Out-of-band flush is in a goroutine; give it a moment.
	deadline := time.Now().Add(500 * time.Millisecond)
	for time.Now().Before(deadline) && sink.totalRows.Load() < 5 {
		time.Sleep(5 * time.Millisecond)
	}
	if got := sink.totalRows.Load(); got != 5 {
		t.Errorf("expected 5 rows written after batch-size trigger, got %d", got)
	}
}

func TestWriteBufferRequeuesOnFailure(t *testing.T) {
	sink := &fakeSink{}
	sink.failures.Store(1) // first InsertBatch returns an error

	b := repository.NewWriteBuffer(sink, repository.WriteBufferConfig{
		FlushInterval: 20 * time.Millisecond,
		BatchSize:     100,
	})

	for i := 0; i < 3; i++ {
		b.Enqueue(sampleRecord(i))
	}

	// First flush fails → batch requeued. Wait for retry timer to fire.
	deadline := time.Now().Add(500 * time.Millisecond)
	for time.Now().Before(deadline) && sink.totalRows.Load() < 3 {
		time.Sleep(10 * time.Millisecond)
	}
	if got := sink.totalRows.Load(); got != 3 {
		t.Errorf("expected 3 rows after retry, got %d", got)
	}
}

func TestWriteBufferStopFlushesPending(t *testing.T) {
	sink := &fakeSink{}
	b := repository.NewWriteBuffer(sink, repository.WriteBufferConfig{
		FlushInterval: 10 * time.Second, // never fires
		BatchSize:     100,
	})

	for i := 0; i < 7; i++ {
		b.Enqueue(sampleRecord(i))
	}

	b.Stop(context.Background())

	if got := sink.totalRows.Load(); got != 7 {
		t.Errorf("Stop should drain pending rows: want 7, got %d", got)
	}
}

func TestWriteBufferStopRefusesNewEnqueues(t *testing.T) {
	sink := &fakeSink{}
	b := repository.NewWriteBuffer(sink, repository.WriteBufferConfig{})
	b.Stop(context.Background())

	b.Enqueue(sampleRecord(1))
	// Allow timers or goroutines a moment to (mis)fire.
	time.Sleep(50 * time.Millisecond)
	if got := sink.totalRows.Load(); got != 0 {
		t.Errorf("Enqueue after Stop should be ignored, but %d rows landed", got)
	}
}

func TestWriteBufferOverflowDropsOldest(t *testing.T) {
	// Use a sink that hangs to keep records pending.
	hanging := &hangingSink{}
	b := repository.NewWriteBuffer(hanging, repository.WriteBufferConfig{
		FlushInterval: time.Hour,
		BatchSize:     1000,
		MaxPending:    3,
	})

	for i := 0; i < 5; i++ {
		b.Enqueue(sampleRecord(i))
	}

	// pending should be capped at 3; the two oldest dropped.
	// We can only assert through the public surface — Stop drains the queue.
	// hangingSink unblocks on the first call.
	hanging.unblockAfter = 1
	b.Stop(context.Background())

	got := hanging.received()
	if len(got) != 3 {
		t.Fatalf("expected 3 records to survive overflow, got %d", len(got))
	}
	// Survivors are records 2, 3, 4 (the oldest two were dropped).
	if got[0].InputTokens != 2 || got[2].InputTokens != 4 {
		t.Errorf("overflow should drop OLDEST: got InputTokens [%d, %d, %d], want [2, 3, 4]",
			got[0].InputTokens, got[1].InputTokens, got[2].InputTokens)
	}
}

type hangingSink struct {
	mu           sync.Mutex
	calls        int
	unblockAfter int
	rows         []repository.MessageRequest
}

func (h *hangingSink) InsertBatch(_ context.Context, batch []repository.MessageRequest) error {
	h.mu.Lock()
	h.calls++
	allow := h.calls >= h.unblockAfter && h.unblockAfter > 0
	if allow {
		h.rows = append(h.rows, batch...)
	}
	h.mu.Unlock()
	if !allow {
		return errors.New("hanging sink: not yet")
	}
	return nil
}

func (h *hangingSink) received() []repository.MessageRequest {
	h.mu.Lock()
	defer h.mu.Unlock()
	out := make([]repository.MessageRequest, len(h.rows))
	copy(out, h.rows)
	return out
}
