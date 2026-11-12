package usage

import (
	"context"
	"log/slog"
	"sync"
	"sync/atomic"
	"time"
)

const (
	queueSize    = 1024
	flushEvery   = time.Second
	flushBatch   = 100
	flushTimeout = 10 * time.Second
	warnEvery    = 30 * time.Second
)

// writer batches records into single inserts so SQLite's one write lock is held briefly and rarely.
type writer struct {
	store *store
	log   *slog.Logger
	now   func() time.Time
	queue chan Record
	stop  chan struct{}
	done  chan struct{}
	once  sync.Once

	batchSize int
	dropped   atomic.Int64
	lastWarn  atomic.Int64 // unix nanos
}

func startWriter(st *store, log *slog.Logger, now func() time.Time, every time.Duration, batch int) *writer {
	w := &writer{
		store: st, log: log, now: now, batchSize: batch,
		queue: make(chan Record, queueSize), stop: make(chan struct{}), done: make(chan struct{}),
	}
	go w.run(every)
	return w
}

func (w *writer) enqueue(r Record) {
	select {
	case w.queue <- r:
	default:
		w.drop()
	}
}

func (w *writer) drop() {
	n := w.dropped.Add(1)
	last := w.lastWarn.Load()
	if now := w.now().UnixNano(); now-last >= int64(warnEvery) && w.lastWarn.CompareAndSwap(last, now) {
		w.log.Warn("usage queue full, records dropped", "dropped_total", n)
	}
}

// close drains the queue, flushes it and waits for the goroutine.
func (w *writer) close() {
	w.once.Do(func() { close(w.stop) })
	<-w.done
}

func (w *writer) run(every time.Duration) {
	defer close(w.done)
	tick := time.NewTicker(every)
	defer tick.Stop()
	var batch []Record
	for {
		select {
		case r := <-w.queue:
			batch = w.add(batch, r)
		case <-tick.C:
			batch = w.flush(batch)
		case <-w.stop:
			w.drain(batch)
			return
		}
	}
}

func (w *writer) add(batch []Record, r Record) []Record {
	if batch = append(batch, r); len(batch) >= w.batchSize {
		return w.flush(batch)
	}
	return batch
}

// drain writes everything still queued.
func (w *writer) drain(batch []Record) {
	for {
		select {
		case r := <-w.queue:
			batch = w.add(batch, r)
		default:
			w.flush(batch)
			return
		}
	}
}

// flush writes the batch and returns it emptied; a failed batch is logged and dropped.
func (w *writer) flush(batch []Record) []Record {
	if len(batch) == 0 {
		return batch
	}
	ctx, cancel := context.WithTimeout(context.Background(), flushTimeout)
	defer cancel()
	if err := w.store.insert(ctx, batch); err != nil {
		w.log.Error("write usage records", "records", len(batch), "err", err)
	}
	return batch[:0]
}
