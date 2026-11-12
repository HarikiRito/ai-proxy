package graph

import (
	"context"

	"github.com/HarikiRito/ai-proxy/backend/internal/modules/health"
	"github.com/HarikiRito/ai-proxy/backend/internal/modules/providers"
	"github.com/HarikiRito/ai-proxy/backend/internal/modules/proxykeys"
	"github.com/HarikiRito/ai-proxy/backend/internal/modules/routing"
	"github.com/HarikiRito/ai-proxy/backend/internal/modules/settings"
	"github.com/HarikiRito/ai-proxy/backend/internal/modules/usage"
	"github.com/HarikiRito/ai-proxy/backend/internal/modules/virtualmodels"
)

//go:generate go tool gqlgen generate

// LiveStatus is the routing engine's view of key load.
type LiveStatus interface {
	Snapshot(ctx context.Context) ([]routing.KeyStatus, error)
}

// Resolver holds module facades only; resolvers never touch stores or ent.
type Resolver struct {
	Health    *health.Service
	Live      LiveStatus
	Providers *providers.Service
	ProxyKeys *proxykeys.Service

	Settings      *settings.Service
	Usage         *usage.Service
	VirtualModels *virtualmodels.Service
}

func deref[T any](p *T) T {
	var zero T
	if p != nil {
		zero = *p
	}
	return zero
}
