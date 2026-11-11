// Package events is a tiny synchronous in-process bus: handlers run in the publisher's goroutine and ctx.
package events

import (
	"context"
	"sync"
)

type Event interface{ Name() string }

type Handler func(ctx context.Context, evt Event) error

type Bus struct {
	mu       sync.RWMutex
	handlers map[string][]Handler
}

func New() *Bus {
	return &Bus{handlers: map[string][]Handler{}}
}

// Subscribe registers h for events whose Name() is name; call during startup only.
func (b *Bus) Subscribe(name string, h Handler) {
	b.mu.Lock()
	defer b.mu.Unlock()
	b.handlers[name] = append(b.handlers[name], h)
}

// Publish runs handlers in subscription order and stops at the first error so the caller can roll back.
func (b *Bus) Publish(ctx context.Context, evt Event) error {
	b.mu.RLock()
	hs := b.handlers[evt.Name()]
	b.mu.RUnlock()
	for _, h := range hs {
		if err := h(ctx, evt); err != nil {
			return err
		}
	}
	return nil
}
