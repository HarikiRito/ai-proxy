package main

import (
	"context"
	"errors"
	"log/slog"
	"net/http"
	"os"
	"os/signal"
	"syscall"
	"time"

	"github.com/HarikiRito/ai-proxy/backend/internal/graph"
	"github.com/HarikiRito/ai-proxy/backend/internal/modules/health"
	"github.com/HarikiRito/ai-proxy/backend/internal/platform/auth"
	"github.com/HarikiRito/ai-proxy/backend/internal/platform/config"
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

	client, pool, err := db.Open(ctx, cfg.DatabaseURL)
	if err != nil {
		return err
	}
	defer func() { _ = client.Close() }()

	healthSvc := health.New(health.Deps{DB: pool})
	resolver := &graph.Resolver{
		Health: healthSvc,
	}

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

	router := httpx.NewRouter(httpx.Options{
		Log: log, AllowedOrigins: cfg.CORSOrigins, Ready: healthSvc.Check, Static: spa,
		WorkOSClientID: cfg.WorkOSClientID, DevMode: cfg.WorkOSDevMode,
	})
	router.Handle("/graphql", authn.Middleware(graph.NewHandler(resolver, cfg.Debug)))
	if cfg.Debug {
		router.Get("/playground", playground.ApolloSandboxHandler("GraphQL", "/graphql"))
	}

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
