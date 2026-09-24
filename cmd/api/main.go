// Command api runs the platform's HTTP API (modular monolith, Phase 1).
package main

import (
	"context"
	"crypto/sha256"
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
	"github.com/redis/go-redis/v9"

	"github.com/johnson11623/social_civic/internal/boundary"
	"github.com/johnson11623/social_civic/internal/identity"
	"github.com/johnson11623/social_civic/internal/membership"
	"github.com/johnson11623/social_civic/internal/moderation"
	"github.com/johnson11623/social_civic/internal/platform/authn"
	"github.com/johnson11623/social_civic/internal/platform/httpjson"
	"github.com/johnson11623/social_civic/internal/platform/i18n"
	"github.com/johnson11623/social_civic/internal/platform/problem"
	"github.com/johnson11623/social_civic/internal/platform/requestid"
	"github.com/johnson11623/social_civic/internal/post"
	"github.com/johnson11623/social_civic/internal/sms"
	"github.com/johnson11623/social_civic/pkg/kms"
	"github.com/johnson11623/social_civic/pkg/ratelimit"
)

type config struct {
	Env           string
	HTTPAddr      string
	DatabaseURL   string
	JWTSigningKey string
	Pepper        string
	PepperVersion string
	RedisURL      string
	PIIKey        string
	RateLimitKey  string
	FeedKey       string
}

func loadConfig() (config, error) {
	c := config{
		Env:           getenv("APP_ENV", "development"),
		HTTPAddr:      getenv("HTTP_ADDR", ":8090"),
		DatabaseURL:   os.Getenv("DATABASE_URL"),
		JWTSigningKey: os.Getenv("JWT_SIGNING_KEY"),
		Pepper:        os.Getenv("NATIONAL_ID_PEPPER"),
		PepperVersion: getenv("NATIONAL_ID_PEPPER_VERSION", "v1"),
		RedisURL:      os.Getenv("REDIS_URL"),
		PIIKey:        os.Getenv("PII_ENCRYPTION_KEY"),
		RateLimitKey:  os.Getenv("RATE_LIMIT_KEY"),
		FeedKey:       os.Getenv("FEED_SIGNING_KEY"),
	}
	switch {
	case c.DatabaseURL == "":
		return c, errors.New("DATABASE_URL is required")
	case c.JWTSigningKey == "":
		return c, errors.New("JWT_SIGNING_KEY is required")
	case c.Pepper == "":
		return c, errors.New("NATIONAL_ID_PEPPER is required")
	case len(c.PIIKey) < 32:
		return c, errors.New("PII_ENCRYPTION_KEY is required (at least 32 bytes)")
	case c.RedisURL == "":
		return c, errors.New("REDIS_URL is required")
	case len(c.RateLimitKey) < 32:
		return c, errors.New("RATE_LIMIT_KEY is required (at least 32 bytes)")
	case len(c.FeedKey) < 32:
		return c, errors.New("FEED_SIGNING_KEY is required (at least 32 bytes)")
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

	redisOpts, err := redis.ParseURL(cfg.RedisURL)
	if err != nil {
		return fmt.Errorf("parse REDIS_URL: %w", err)
	}
	rdb := redis.NewClient(redisOpts)
	defer rdb.Close()
	if err := rdb.Ping(ctx).Err(); err != nil {
		return fmt.Errorf("ping redis: %w", err)
	}
	limiter := &ratelimit.Redis{Client: rdb}
	tooManyRequests := func(w http.ResponseWriter, r *http.Request) {
		problem.Write(w, r, http.StatusTooManyRequests, "rate_limited", i18n.MsgRateLimited)
	}
	byIP := ratelimit.ByClientIP([]byte(cfg.RateLimitKey))
	// API Spec §1.6 rate class "auth": 5 registrations per IP per hour (T-1.1.1.10).
	limit := func(name string, n int, window time.Duration) func(http.Handler) http.Handler {
		return ratelimit.Middleware(limiter, ratelimit.Rule{Name: name, Limit: n, Window: window}, byIP, tooManyRequests, logger)
	}
	registerLimit := limit("register", 5, time.Hour)  // T-1.1.1.10
	otpLimit := limit("otp", 5, 15*time.Minute)       // SMS cost and abuse
	loginLimit := limit("login", 5, 5*time.Minute)    // T-1.1.2.7
	refreshLimit := limit("refresh", 10, time.Minute) // F-05

	tree, err := boundary.LoadTree(ctx, pool)
	if err != nil {
		return err
	}
	boundaryAPI := boundary.NewHandlers(tree)

	keyring := kms.NewStatic()
	keyring.Set(identity.PepperKeyName, []byte(cfg.Pepper), cfg.PepperVersion)
	piiKey := sha256.Sum256([]byte(cfg.PIIKey)) // dev keyring: derive the 32-byte AES key
	keyring.Set(identity.PIIKeyName, piiKey[:], "dev-v1")

	tokens, err := identity.NewTokenIssuer([]byte(cfg.JWTSigningKey), time.Now)
	if err != nil {
		return err
	}

	identityStore := identity.NewPostgresStore(pool)
	register := &identity.RegisterHandler{
		Store:    identityStore,
		Sessions: identityStore,
		Keyring:  keyring,
		Boundary: tree,
		Tokens:   tokens,
		Logger:   logger,
		Now:      time.Now,
	}

	auth := &identity.AuthHandlers{
		Store:   identityStore,
		Keyring: keyring,
		Tokens:  tokens,
		SMS:     sms.DevLogSender{Logger: logger}, // carrier adapter: EPIC 4.5
		Limiter: limiter,
		Logger:  logger,
		Now:     time.Now,
	}

	consent := &identity.ConsentHandlers{Store: identityStore, Logger: logger, Now: time.Now}
	erasure := &identity.ErasureHandlers{Store: identityStore, Logger: logger, Now: time.Now}
	requireAuth := authn.Middleware(tokens, identity.Unauthenticated)
	requireConsent := identity.RequireConsent(identityStore, logger)
	profile := &identity.ProfileHandlers{Pool: pool, Boundary: tree, Logger: logger}
	mfa := &identity.MFAHandlers{Pool: pool, Keyring: keyring, Tokens: tokens, Logger: logger, Now: time.Now}
	roles := &membership.Handlers{Store: membership.NewStore(pool), Logger: logger, Now: time.Now}
	channels := &post.ChannelHandlers{Store: post.NewStore(pool), Wards: tree, Logger: logger}
	// Per-user limit on channel creation (spam), after authentication.
	perUser := func(name string, n int, window time.Duration) func(http.Handler) http.Handler {
		return ratelimit.Middleware(limiter, ratelimit.Rule{Name: name, Limit: n, Window: window}, post.KeyByUser, tooManyRequests, logger)
	}
	channelLimit := perUser("channel", 5, time.Hour)
	// T-2.1.2.4 — 10 posts a minute and 100 a day per user.
	postMinute, postDay := perUser("post-min", 10, time.Minute), perUser("post-day", 100, 24*time.Hour)
	// T-2.1.3.7 — 60 likes and 20 replies a minute per user.
	likeLimit, replyLimit := perUser("like", 60, time.Minute), perUser("reply", 20, time.Minute)
	feedCache := post.RedisFeedCache{Client: rdb, Key: []byte(cfg.FeedKey)}
	mod := &moderation.Handlers{Pool: pool, Posts: post.NewStore(pool), Members: roles, Cache: feedCache, Logger: logger, Now: time.Now}
	posts := &post.PostHandlers{Store: post.NewStore(pool), Cache: feedCache, Logger: logger}
	feed := &post.FeedHandlers{Store: post.NewStore(pool), Cache: feedCache, Logger: logger}

	r := chi.NewRouter()
	r.Use(requestid.Middleware, middleware.Recoverer)
	r.Get("/v1/health", func(w http.ResponseWriter, _ *http.Request) {
		httpjson.Write(w, http.StatusOK, map[string]string{"status": "ok", "time": time.Now().UTC().Format(time.RFC3339)})
	})
	r.Get("/v1/boundary/tree", boundaryAPI.GetTree)
	r.Get("/v1/boundary/search", boundaryAPI.Search)
	r.With(registerLimit).Method(http.MethodPost, "/v1/auth/register", register)
	r.With(otpLimit).Post("/v1/auth/otp", auth.RequestOTP)
	r.With(loginLimit).Post("/v1/auth/login", auth.Login)
	r.With(refreshLimit).Post("/v1/auth/refresh", auth.Refresh)
	r.With(requireAuth).Post("/v1/users/me/consent/withdraw", consent.Withdraw)
	r.With(requireAuth).Post("/v1/users/me/erasure", erasure.Request)

	// Channel & Post (EPIC 2.1). Writes process personal data, so they also
	// require active consent (T-1.1.3.3).
	// F-08 — TOTP enrolment and step-up; 5 codes per 15 minutes per user.
	mfaLimit := perUser("mfa", 5, 15*time.Minute)
	requireMFA := authn.RequireMFA(identity.MFARequired)
	r.With(requireAuth).Get("/v1/users/me", profile.Get)
	r.With(requireAuth, perUser("profile", 20, time.Hour)).Patch("/v1/users/me", profile.Update)
	r.With(requireAuth).Get("/v1/users/me/mfa", mfa.Status)
	r.With(requireAuth, mfaLimit).Post("/v1/users/me/mfa/totp", mfa.Enrol)
	r.With(requireAuth, mfaLimit).Post("/v1/users/me/mfa/totp/verify", mfa.Activate)
	r.With(requireAuth, mfaLimit).Post("/v1/auth/mfa", mfa.StepUp)

	r.With(requireAuth).Get("/v1/users/me/roles", roles.MyRoles)
	r.With(requireAuth, requireMFA).Post("/v1/moderators", roles.Appoint)
	// LLD §8 — 10 reports an hour per user.
	r.With(requireAuth, requireConsent, perUser("report", 10, time.Hour)).Post("/v1/reports", mod.Report)
	// API spec §1.6 — 100 moderation actions an hour per moderator.
	r.With(requireAuth, requireMFA, perUser("moderation", 100, time.Hour)).Post("/v1/moderation/actions", mod.Act)
	r.With(requireAuth).Get("/v1/moderation/queue", mod.Queue)
	r.With(requireAuth).Get("/v1/posts/{post_id}/moderation", mod.History)
	r.With(requireAuth, requireConsent, perUser("appeal", 5, 24*time.Hour)).Post("/v1/appeals", mod.File) // LLD §8: 5 a day
	r.With(requireAuth).Get("/v1/appeals", mod.Open)
	r.With(requireAuth, requireMFA).Post("/v1/appeals/{appeal_id}/decision", mod.Decide)
	r.With(requireAuth).Get("/v1/channels", channels.List)
	r.With(requireAuth).Get("/v1/channels/{channel_id}", channels.Get)
	r.With(requireAuth).Get("/v1/channels/{channel_id}/posts", channels.Posts)
	r.With(requireAuth, requireConsent, channelLimit).Post("/v1/channels", channels.Create)
	r.With(requireAuth, requireConsent, postMinute, postDay).Post("/v1/channels/{channel_id}/posts", posts.Create)
	r.With(requireAuth).Get("/v1/feed", feed.Feed)
	r.With(requireAuth).Get("/v1/posts/{post_id}", posts.Get)
	r.With(requireAuth).Get("/v1/posts/{post_id}/replies", posts.Replies)
	r.With(requireAuth, requireConsent, likeLimit).Post("/v1/posts/{post_id}/likes", posts.Like)
	r.With(requireAuth, requireConsent, likeLimit).Delete("/v1/posts/{post_id}/likes", posts.Unlike)
	r.With(requireAuth, requireConsent, replyLimit).Post("/v1/posts/{post_id}/replies", posts.Reply)

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
		logger.Info("api listening", "addr", cfg.HTTPAddr, "env", cfg.Env, "boundary_version", tree.Version, "boundary_units", tree.Len())
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
