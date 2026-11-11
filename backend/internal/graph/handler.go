package graph

import (
	"context"
	"errors"
	"log/slog"
	"net/http"

	"github.com/99designs/gqlgen/graphql"
	"github.com/99designs/gqlgen/graphql/handler"
	"github.com/99designs/gqlgen/graphql/handler/extension"
	"github.com/99designs/gqlgen/graphql/handler/lru"
	"github.com/99designs/gqlgen/graphql/handler/transport"
	"github.com/vektah/gqlparser/v2/ast"
	"github.com/vektah/gqlparser/v2/gqlerror"

	"github.com/HarikiRito/ai-proxy/backend/internal/modules/providers"
	"github.com/HarikiRito/ai-proxy/backend/internal/modules/proxykeys"
	"github.com/HarikiRito/ai-proxy/backend/internal/modules/virtualmodels"
)

func NewHandler(r *Resolver, debug bool) http.Handler {
	h := handler.New(NewExecutableSchema(Config{Resolvers: r}))
	h.AddTransport(transport.POST{})
	h.SetQueryCache(lru.New[*ast.QueryDocument](1000))
	h.SetErrorPresenter(presentError)
	if debug {
		h.Use(extension.Introspection{})
	}
	return h
}

// presentError hides internal errors from clients and logs them.
func presentError(ctx context.Context, err error) *gqlerror.Error {
	gqlErr := graphql.DefaultErrorPresenter(ctx, err)
	if msg, ok := userMessage(err); ok {
		gqlErr.Message = msg
		return gqlErr
	}
	if gqlErr.Err != nil { // parse and validation errors carry no wrapped error and are safe to show
		slog.ErrorContext(ctx, "graphql", "err", err)
		gqlErr.Message = "internal server error"
	}
	return gqlErr
}

// userMessage returns the message of a module UserError, which is safe to show to the owner.
func userMessage(err error) (string, bool) {
	var providerErr *providers.UserError
	var keyErr *proxykeys.UserError
	var modelErr *virtualmodels.UserError
	switch {
	case errors.As(err, &providerErr):
		return providerErr.Msg, true
	case errors.As(err, &keyErr):
		return keyErr.Msg, true
	case errors.As(err, &modelErr):
		return modelErr.Msg, true
	}
	return "", false
}
