// Package health reports whether the service can reach its dependencies.
package health

import "context"

type Pinger interface {
	PingContext(ctx context.Context) error
}

type Deps struct {
	DB Pinger
}

type Service struct{ db Pinger }

func New(d Deps) *Service {
	return &Service{db: d.DB}
}

func (s *Service) Check(ctx context.Context) error {
	return s.db.PingContext(ctx)
}
