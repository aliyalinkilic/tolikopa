// Command server is the Tolikopa backend: agent ingest + REST/WS API + alert engine.
package main

import (
	"context"
	"errors"
	"log"
	"net/http"
	"os"
	"os/signal"
	"syscall"
	"time"

	"github.com/aliyalinkilic/tolikopa/server/internal/admincli"
	"github.com/aliyalinkilic/tolikopa/server/internal/alerts"
	"github.com/aliyalinkilic/tolikopa/server/internal/api"
	"github.com/aliyalinkilic/tolikopa/server/internal/appcatalog"
	"github.com/aliyalinkilic/tolikopa/server/internal/auth"
	"github.com/aliyalinkilic/tolikopa/server/internal/config"
	"github.com/aliyalinkilic/tolikopa/server/internal/db"
	"github.com/aliyalinkilic/tolikopa/server/internal/ingest"
	"github.com/aliyalinkilic/tolikopa/server/internal/ws"
)

func main() {
	log.SetFlags(log.LstdFlags | log.LUTC)

	// Break-glass account recovery: `server admin <command>` works directly on
	// the database and exits. It needs only DATABASE_URL, so it is dispatched
	// before the full configuration is loaded (docs/SECURITY.md).
	if len(os.Args) > 1 && os.Args[1] == "admin" {
		os.Exit(admincli.Main(context.Background(), os.Args[2:]))
	}

	cfg, err := config.Load()
	if err != nil {
		log.Fatalf("config: %v", err)
	}

	// Root context cancelled on SIGINT/SIGTERM for graceful shutdown.
	ctx, stop := signal.NotifyContext(context.Background(), syscall.SIGINT, syscall.SIGTERM)
	defer stop()

	// Database: connect (with retry) + run embedded migrations.
	startCtx, cancel := context.WithTimeout(ctx, 2*time.Minute)
	pool, err := db.Connect(startCtx, cfg.DatabaseURL)
	if err != nil {
		cancel()
		log.Fatalf("db connect: %v", err)
	}
	defer pool.Close()
	if err := db.Migrate(startCtx, pool); err != nil {
		cancel()
		log.Fatalf("db migrate: %v", err)
	}
	cancel()
	log.Printf("db: migrations applied")

	// Auth + first-admin seed.
	authSvc := auth.New(pool, cfg)
	// Case-insensitive username uniqueness at the schema level (see 0016).
	if err := authSvc.EnsureUsernameIndex(ctx); err != nil {
		log.Fatalf("username index: %v", err)
	}
	if err := authSvc.SeedAdmin(ctx); err != nil {
		log.Fatalf("seed admin: %v", err)
	}
	// No account at all (fresh install without ADMIN_* env): arm the one-time
	// setup code that lets the installer create the first admin in the browser.
	if err := authSvc.InitSetup(ctx); err != nil {
		log.Fatalf("first-run setup: %v", err)
	}

	// Remediation catalog mirror. The vocabulary lives in the binary (embedded,
	// generated from the agent's canonical copy); this only refreshes the table the
	// panel reads and retires ids the binary no longer carries. Deliberately not a
	// migration: catalogs churn, migrations are append-only.
	if err := appcatalog.Sync(ctx, pool); err != nil {
		log.Fatalf("appcatalog sync: %v", err)
	}

	// Classification cache.
	cats, err := ingest.NewCategories(ctx, pool)
	if err != nil {
		log.Fatalf("categories: %v", err)
	}

	// Remediation policy snapshot (resolved per machine at ingest; refreshed on
	// admin write plus a periodic safety-net reload).
	pols, err := ingest.NewPolicies(ctx, pool)
	if err != nil {
		log.Fatalf("remediation policy: %v", err)
	}

	// Components.
	ingester := ingest.NewIngester(pool, cats, pols, cfg)
	hub := ws.NewHub(pool, cfg, authSvc)
	engine := alerts.NewEngine(pool, cfg, hub)
	// A WebSocket is authenticated once, at upgrade; drop a user's live
	// connections the moment their sessions are revoked.
	authSvc.OnSessionsRevoked(hub.DisconnectUser)

	go hub.Run(ctx)
	go engine.Run(ctx)
	go pols.Run(ctx)

	// HTTP server.
	apiSvc := api.New(pool, cfg, cats, pols, authSvc, ingester, hub)
	e := apiSvc.Router()

	go func() {
		log.Printf("server: listening on %s", cfg.ListenAddr)
		if err := e.Start(cfg.ListenAddr); err != nil && !errors.Is(err, http.ErrServerClosed) {
			log.Fatalf("server: %v", err)
		}
	}()

	// Wait for shutdown signal.
	<-ctx.Done()
	log.Printf("server: shutting down")

	shutdownCtx, shutdownCancel := context.WithTimeout(context.Background(), 15*time.Second)
	defer shutdownCancel()
	if err := e.Shutdown(shutdownCtx); err != nil {
		log.Printf("server: shutdown error: %v", err)
	}
}
