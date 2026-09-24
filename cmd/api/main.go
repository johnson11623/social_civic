// Command api runs the platform's HTTP API (modular monolith, Phase 1).
package main

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"net/http"
	"os"
	"os/signal"
	"syscall"
	"time"

	"github.com/go-chi/chi/v5"
	"github.com/go-chi/chi/v5/middleware"
	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/johnson11623/social_civic/internal/boundary"
	"github.com/johnson11623/social_civic/internal/identity"
	"github.com/johnson11623/social_civic/internal/platform/httpjson"
	"github.com/johnson11623/social_civic/internal/platform/requestid"
	"github.com/johnson11623/social_civic/pkg/events"
	"github.com/johnson11623/social_civic/pkg/kms"
)

type config struct {
	Env           string
	HTTPAddr      string
	DatabaseURL   string
	JWTSigningKey string
	Pepper        string
	PepperVersion string
	BoundaryFile  string
}

func loadConfig() (config, error) {
	c := config{
		Env:           getenv("APP_ENV", "development"),
		HTTPAddr:      getenv("HTTP_ADDR", ":8090"),
		DatabaseURL:   os.Getenv("DATABASE_URL"),
		JWTSigningKey: os.Getenv("JWT_SIGNING_KEY"),
		Pepper:        os.Getenv("NATIONAL_ID_PEPPER"),
		PepperVersion: getenv("NATIONAL_ID_PEPPER_VERSION", "v1"),
		BoundaryFile:  getenv("BOUNDARY_FILE", "db/seed/boundary_dev_fixture.json"),
	}
	switch {
	case c.DatabaseURL == "":
		return c, errors.New("DATABASE_URL is required")
	case c.JWTSigningKey == "":
		return c, errors.New("JWT_SIGNING_KEY is required")
	case c.Pepper == "":
		return c, errors.New("NATIONAL_ID_PEPPER is required")
	}
	// F-01: the env-based pepper and static keyring are for development only.
	if c.Env == "production" {
		return c, errors.New("production requires a KMS/Vault keyring (T-X.4); env peppers are not allowed")
	}
	return c, nil
}

func getenv(key, fallback string) string {
	if v := os.Getenv(key); v != "" {
		return v
	}
	return fallback
}

func main() {
	logger := slog.New(slog.NewJSONHandler(os.Stdout, nil))
	if err := run(logger); err != nil {
		logger.Error("api exited", "err", err)
		os.Exit(1)
	}
}

func run(logger *slog.Logger) error {
	cfg, err := loadConfig()
	if err != nil {
		return err
	}

	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()

	pool, err := pgxpool.New(ctx, cfg.DatabaseURL)
	if err != nil {
		return fmt.Errorf("connect database: %w", err)
	}
	defer pool.Close()
	if err := pool.Ping(ctx); err != nil {
		return fmt.Errorf("ping database: %w", err)
	}

	tree, err := boundary.LoadFile(cfg.BoundaryFile)
	if err != nil {
		return err
	}

	keyring := kms.NewStatic()
	keyring.Set(identity.PepperKeyName, []byte(cfg.Pepper), cfg.PepperVersion)

	tokens, err := identity.NewTokenIssuer([]byte(cfg.JWTSigningKey), time.Now)
	if err != nil {
		return err
	}

	register := &identity.RegisterHandler{
		Store:    identity.NewPostgresStore(pool),
		Keyring:  keyring,
		Boundary: tree,
		Tokens:   tokens,
		Events:   events.LogPublisher{Logger: logger},
		Logger:   logger,
		Now:      time.Now,
	}

	r := chi.NewRouter()
	r.Use(requestid.Middleware, middleware.Recoverer)
	r.Get("/v1/health", func(w http.ResponseWriter, _ *http.Request) {
		httpjson.Write(w, http.StatusOK, map[string]string{"status": "ok", "time": time.Now().UTC().Format(time.RFC3339)})
	})
	r.Method(http.MethodPost, "/v1/auth/register", register)

	srv := &http.Server{
		Addr:              cfg.HTTPAddr,
		Handler:           r,
		ReadHeaderTimeout: 5 * time.Second,
		ReadTimeout:       15 * time.Second,
		WriteTimeout:      30 * time.Second,
		IdleTimeout:       60 * time.Second,
		MaxHeaderBytes:    1 << 20,
	}

	errc := make(chan error, 1)
	go func() {
		logger.Info("api listening", "addr", cfg.HTTPAddr, "env", cfg.Env, "boundary_version", tree.Version)
		errc <- srv.ListenAndServe()
	}()

	select {
	case err := <-errc:
		if !errors.Is(err, http.ErrServerClosed) {
			return err
		}
		return nil
	case <-ctx.Done():
	}

	shutdownCtx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	logger.Info("api shutting down")
	return srv.Shutdown(shutdownCtx)
}
