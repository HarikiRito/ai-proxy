package graph

import (
	"github.com/HarikiRito/ai-proxy/backend/internal/modules/health"
	"github.com/HarikiRito/ai-proxy/backend/internal/modules/providers"
)

//go:generate go tool gqlgen generate

// Resolver holds module facades only; resolvers never touch stores or ent.
type Resolver struct {
	Health    *health.Service
	Providers *providers.Service
}
