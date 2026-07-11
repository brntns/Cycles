// Command server runs the Varde API and serves the built PWA as
// static files from the same process.
package main

import (
	"context"
	"log"
	"mime"
	"net/http"
	"os"
	"os/signal"
	"syscall"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"

	"varde/internal/auth"
	"varde/internal/config"
	"varde/internal/db"
	"varde/internal/dbmig"
	"varde/internal/httpapi"
	"varde/web"
)

func main() {
	if err := run(); err != nil {
		log.Fatalf("fatal: %v", err)
	}
}

func run() error {
	_ = mime.AddExtensionType(".webmanifest", "application/manifest+json")

	cfg, err := config.Load()
	if err != nil {
		return err
	}

	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()

	pool, err := connectWithRetry(ctx, cfg.DatabaseURL, 10, 2*time.Second)
	if err != nil {
		return err
	}
	defer pool.Close()

	log.Println("running migrations...")
	if err := dbmig.Run(ctx, pool); err != nil {
		return err
	}

	authSvc := auth.NewService(pool, cfg.Password, cfg.CookieSecure, cfg.SessionMaxDays)

	staticFS, err := web.StaticFS()
	if err != nil {
		return err
	}

	server := httpapi.NewServer(pool, authSvc, staticFS)

	httpServer := &http.Server{
		Addr:              ":" + cfg.Port,
		Handler:           server.Routes(),
		ReadHeaderTimeout: 10 * time.Second,
	}

	go func() {
		<-ctx.Done()
		shutdownCtx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
		defer cancel()
		_ = httpServer.Shutdown(shutdownCtx)
	}()

	log.Printf("listening on :%s", cfg.Port)
	if err := httpServer.ListenAndServe(); err != nil && err != http.ErrServerClosed {
		return err
	}
	return nil
}

func connectWithRetry(ctx context.Context, url string, attempts int, delay time.Duration) (*pgxpool.Pool, error) {
	var lastErr error
	for i := 0; i < attempts; i++ {
		pool, err := db.Connect(ctx, url)
		if err == nil {
			return pool, nil
		}
		lastErr = err
		log.Printf("db connect attempt %d/%d failed: %v", i+1, attempts, err)
		select {
		case <-ctx.Done():
			return nil, ctx.Err()
		case <-time.After(delay):
		}
	}
	return nil, lastErr
}
