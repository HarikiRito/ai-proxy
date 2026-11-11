package main

import (
	"context"
	"errors"
	"io/fs"
	"log/slog"
	"net/http"
	"os"
	"os/signal"
	"syscall"
	"time"

	"github.com/HarikiRito/ai-proxy/backend/internal/ent"
	"github.com/HarikiRito/ai-proxy/backend/internal/graph"
	"github.com/HarikiRito/ai-proxy/backend/internal/modules/health"
	"github.com/HarikiRito/ai-proxy/backend/internal/modules/providers"
	"github.com/HarikiRito/ai-proxy/backend/internal/modules/proxykeys"
	"github.com/HarikiRito/ai-proxy/backend/internal/modules/virtualmodels"
	"github.com/HarikiRito/ai-proxy/backend/internal/platform/auth"
	"github.com/HarikiRito/ai-proxy/backend/internal/platform/config"
	"github.com/HarikiRito/ai-proxy/backend/internal/platform/crypto"
	"github.com/HarikiRito/ai-proxy/backend/internal/platform/db"
	"github.com/HarikiRito/ai-proxy/backend/internal/platform/httpx"
	"github.com/HarikiRito/ai-proxy/backend/internal/platform/logging"
	"github.com/HarikiRito/ai-proxy/web"

	"github.com/99designs/gqlgen/graphql/playground"
)

const shutdownTimeout = 10 * time.Second

func main() {
	if err := run(); err != nil {
		slog.Error("fatal", "err", err)
		os.Exit(1)
	}
}

func run() error {
	cfg, err := config.Load()
	if err != nil {
		return err
	}
	log := logging.New(cfg.Debug)
	ctx, stop := signal.NotifyContext(context.Background(), syscall.SIGINT, syscall.SIGTERM)
	defer stop()

	cipher, err := crypto.New(cfg.MasterKey)
	if err != nil {
		return err
	}

	client, pool, err := db.Open(ctx, cfg.DatabaseURL)
	if err != nil {
		return err
	}
	defer func() { _ = client.Close() }()

	healthSvc := health.New(health.Deps{DB: pool})
	resolver := newResolver(client, healthSvc, cipher)

	spa, err := web.Build()
	if err != nil {
		return err
	}

	authn, err := auth.New(ctx, auth.Config{
		ClientID:   cfg.WorkOSClientID,
		APIKey:     cfg.WorkOSAPIKey,
		Issuer:     cfg.WorkOSIssuer,
		OwnerEmail: cfg.OwnerEmail,
	})
	if err != nil {
		return err
	}

	router := newRouter(cfg, log, authn, spa, resolver, healthSvc)

	srv := &http.Server{Addr: ":" + cfg.Port, Handler: router, ReadHeaderTimeout: 10 * time.Second}
	serveErr := make(chan error, 1)
	go func() { serveErr <- srv.ListenAndServe() }()
	log.Info("listening", "addr", srv.Addr)

	select {
	case err := <-serveErr:
		return err
	case <-ctx.Done():
	}
	shutdownCtx, cancel := context.WithTimeout(context.Background(), shutdownTimeout)
	defer cancel()
	if err := srv.Shutdown(shutdownCtx); err != nil && !errors.Is(err, http.ErrServerClosed) {
		return err
	}
	return nil
}

func newResolver(client *ent.Client, healthSvc *health.Service, cipher providers.Cipher) *graph.Resolver {
	providerSvc := providers.New(providers.Deps{DB: client, Cipher: cipher})
	return &graph.Resolver{
		Health:        healthSvc,
		Providers:     providerSvc,
		ProxyKeys:     proxykeys.New(proxykeys.Deps{DB: client}),
		VirtualModels: virtualmodels.New(virtualmodels.Deps{DB: client, Providers: providerDirectory{providerSvc}}),
	}
}

// providerDirectory adapts providers.Service to the interface virtualmodels consumes.
type providerDirectory struct{ svc *providers.Service }

func (d providerDirectory) ProviderInfo(ctx context.Context, ids []string) (map[string]virtualmodels.ProviderInfo, error) {
	found, err := d.svc.ByIDs(ctx, ids)
	if err != nil {
		return nil, err
	}
	out := make(map[string]virtualmodels.ProviderInfo, len(found))
	for id, p := range found {
		out[id] = virtualmodels.ProviderInfo{Name: p.Name, Kind: string(p.Kind), Enabled: p.Enabled}
	}
	return out, nil
}

func newRouter(cfg config.Config, log *slog.Logger, authn *auth.Authenticator, spa fs.FS, resolver *graph.Resolver, healthSvc *health.Service) http.Handler {
	router := httpx.NewRouter(httpx.Options{
		Log: log, AllowedOrigins: cfg.CORSOrigins, Ready: healthSvc.Check, Static: spa,
		WorkOSClientID: cfg.WorkOSClientID, DevMode: cfg.WorkOSDevMode,
	})
	router.Handle("/graphql", authn.Middleware(graph.NewHandler(resolver, cfg.Debug)))
	if cfg.Debug {
		router.Get("/playground", playground.ApolloSandboxHandler("GraphQL", "/graphql"))
	}
	return router
}
