package main

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"net/http"
	"os"
	"os/signal"
	"sync" // Now includes the .Go() method
	"syscall"
	"time"

	"github.com/gin-gonic/gin"
	_ "github.com/joho/godotenv/autoload"
	"github.com/nourabuild/relays-api/internal/app"
	"github.com/nourabuild/relays-api/internal/sdk/config"
	"github.com/nourabuild/relays-api/internal/sdk/debug"
	"github.com/nourabuild/relays-api/internal/sdk/sqldb"
	"github.com/nourabuild/relays-api/internal/services/jwt"
	"github.com/nourabuild/relays-api/internal/services/sentry"
)

var build string

func main() {
	// 1. Optimized Logging: Defaulting to JSON for production
	logger := slog.New(slog.NewJSONHandler(os.Stdout, nil))
	slog.SetDefault(logger)

	if err := run(logger); err != nil {
		logger.Error("application startup failed", "error", err)
		os.Exit(1)
	}
}

func run(logger *slog.Logger) error {
	ctx, stop := signal.NotifyContext(context.Background(), syscall.SIGINT, syscall.SIGTERM)
	defer stop()

	// Gin defaults to debug mode; production noise unless explicitly chosen.
	if _, ok := os.LookupEnv("GIN_MODE"); !ok {
		gin.SetMode(gin.ReleaseMode)
	}

	// 2. Resource Management with WaitGroups
	var wg sync.WaitGroup

	// 3. Load and validate all configuration up front; a misconfigured
	// service must not start.
	cfg, err := config.Load()
	if err != nil {
		return fmt.Errorf("config: %w", err)
	}

	// 4. Initialize Database service
	sqlService, err := sqldb.New(cfg.DB)
	if err != nil {
		return fmt.Errorf("database: %w", err)
	}
	defer sqlService.Close()

	// 5. Initialize Sentry for error tracking
	sentryService := sentry.NewSentryService(cfg.Sentry)
	defer sentryService.Close()

	// 6. App Initialization
	relaysApp := app.NewApp(
		cfg,
		sqlService,
		sentryService,
		jwt.NewTokenService(cfg.JWT.Secret, cfg.JWT.Issuer),
	)

	// 7. Server with configured timeouts
	srv := &http.Server{
		Addr:         ":" + cfg.HTTP.Port,
		Handler:      relaysApp.RegisterRoutes(),
		IdleTimeout:  time.Minute,
		ReadTimeout:  5 * time.Second,
		WriteTimeout: 10 * time.Second,
		ErrorLog:     slog.NewLogLogger(logger.Handler(), slog.LevelError),
	}

	// 8. Debug/pprof server, loopback only: never expose beyond the host/pod.
	debugSrv := &http.Server{
		Addr:         "127.0.0.1:" + cfg.HTTP.DebugPort,
		Handler:      debug.Mux(),
		IdleTimeout:  time.Minute,
		ReadTimeout:  5 * time.Second,
		WriteTimeout: 10 * time.Second,
		ErrorLog:     slog.NewLogLogger(logger.Handler(), slog.LevelError),
	}

	wg.Go(func() {
		logger.Info("server starting", "addr", srv.Addr, "build", build)
		if err := srv.ListenAndServe(); err != nil && !errors.Is(err, http.ErrServerClosed) {
			logger.Error("listen and serve", "error", err)
			stop() // Cancel context if server crashes
		}
	})

	wg.Go(func() {
		logger.Info("debug server starting", "addr", debugSrv.Addr)
		if err := debugSrv.ListenAndServe(); err != nil && !errors.Is(err, http.ErrServerClosed) {
			logger.Error("debug server", "error", err)
		}
	})

	// 9. Graceful Shutdown Wait
	<-ctx.Done()
	logger.Info("shutting down gracefully")

	shutdownCtx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
	defer cancel()

	if err := srv.Shutdown(shutdownCtx); err != nil {
		return fmt.Errorf("shutdown: %w", err)
	}

	if err := debugSrv.Shutdown(shutdownCtx); err != nil {
		logger.Error("debug server shutdown", "error", err)
	}

	logger.Info("shutdown complete")
	return nil
}
