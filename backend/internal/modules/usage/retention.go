package usage

import (
	"context"
	"time"
)

const (
	retention   = 30 * 24 * time.Hour
	retainEvery = 24 * time.Hour
	retainChunk = 1000
)

// Start purges old records now and daily until ctx ends or Close runs.
func (s *Service) Start(ctx context.Context) {
	ctx, cancel := context.WithCancel(ctx)
	s.mu.Lock()
	s.cancel = cancel
	s.mu.Unlock()
	ticker := time.NewTicker(retainEvery)
	s.jobs.Add(1)
	go func() {
		defer s.jobs.Done()
		defer ticker.Stop()
		s.retain(ctx, ticker.C)
	}()
}

func (s *Service) retain(ctx context.Context, tick <-chan time.Time) {
	for {
		if err := s.store.deleteBefore(ctx, s.now().Add(-retention), retainChunk); err != nil && ctx.Err() == nil {
			s.writer.log.Error("purge usage records", "err", err)
		}
		select {
		case <-ctx.Done():
			return
		case <-tick:
		}
	}
}
