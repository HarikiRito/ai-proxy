package graph

import (
	"github.com/HarikiRito/ai-proxy/backend/internal/modules/health"
	"github.com/HarikiRito/ai-proxy/backend/internal/modules/providers"
	"github.com/HarikiRito/ai-proxy/backend/internal/modules/proxykeys"
	"github.com/HarikiRito/ai-proxy/backend/internal/modules/settings"
	"github.com/HarikiRito/ai-proxy/backend/internal/modules/usage"
	"github.com/HarikiRito/ai-proxy/backend/internal/modules/virtualmodels"
)

//go:generate go tool gqlgen generate

// Resolver holds module facades only; resolvers never touch stores or ent.
type Resolver struct {
	Health    *health.Service
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
