package main

import (
	"context"
	"log"
	"net/http"
	"os"
	"os/signal"
	"syscall"
	"time"

	"github.com/pingsantohq/controller/internal/server"
	"github.com/pingsantohq/controller/internal/store"
)

func main() {
	logger := log.New(os.Stdout, "controller ", log.LstdFlags|log.Lmicroseconds)

	ctx := context.Background()
	var (
		st      store.Store
		cleanup func()
	)

	dbURL := os.Getenv("DATABASE_URL")
	if dbURL != "" {
		pgStore, err := store.NewPostgresStore(ctx, dbURL)
		if err != nil {
			logger.Fatalf("failed to connect to database: %v", err)
		}
		st = pgStore
		cleanup = func() { pgStore.Close() }
		logger.Println("upgrade API using PostgreSQL store")
	} else {
		st = store.NewMemoryStore()
		cleanup = func() {}
		logger.Println("DATABASE_URL not set, using in-memory store (not for production)")
	}
	defer cleanup()

	cfg := server.Config{
		Addr:             getenvDefault("LISTEN_ADDR", ":8080"),
		ReadTimeout:      5 * time.Second,
		WriteTimeout:     10 * time.Second,
		IdleTimeout:      60 * time.Second,
		AgentAuthMode:    getenvDefault("AGENT_AUTH_MODE", "header"),
		AdminBearerToken: os.Getenv("ADMIN_BEARER_TOKEN"),
	}

	srv := server.New(cfg, server.Dependencies{
		Logger: logger,
		Store:  st,
	})

	shutdownCtx, stop := signal.NotifyContext(ctx, syscall.SIGINT, syscall.SIGTERM)
	defer stop()

	serverErr := make(chan error, 1)
	go func() {
		logger.Printf("starting controller on %s", srv.Addr)
		if err := srv.ListenAndServe(); err != nil && err != http.ErrServerClosed {
			serverErr <- err
		}
	}()

	select {
	case <-shutdownCtx.Done():
		logger.Println("shutdown signal received")
	case err := <-serverErr:
		logger.Fatalf("server error: %v", err)
	}

	ctxTimeout, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	if err := srv.Shutdown(ctxTimeout); err != nil {
		logger.Printf("graceful shutdown failed: %v", err)
	}
	logger.Println("controller stopped")
}

func getenvDefault(key, def string) string {
	if val := os.Getenv(key); val != "" {
		return val
	}
	return def
}
