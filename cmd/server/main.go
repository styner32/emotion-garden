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

	"emotion-garden/internal/ai"
	"emotion-garden/internal/ai/gemini"
	"emotion-garden/internal/ai/mock"
	"emotion-garden/internal/ai/openai"
	"emotion-garden/internal/character"
	"emotion-garden/internal/config"
	"emotion-garden/internal/conversation"
	"emotion-garden/internal/garden"
	"emotion-garden/internal/httpapi"
	"emotion-garden/internal/identity"
	"emotion-garden/internal/ritual"
	"emotion-garden/internal/storage"
)

func main() {
	logger := slog.New(slog.NewJSONHandler(os.Stdout, &slog.HandlerOptions{
		Level: slog.LevelInfo,
	}))

	bundle, err := config.LoadFromEnv()
	if err != nil {
		logger.Error("failed to load configuration", "err_code", "CONFIG_LOAD_ERROR")
		os.Exit(1)
	}

	appCtx, stopSignals := signal.NotifyContext(context.Background(), syscall.SIGINT, syscall.SIGTERM)
	defer stopSignals()

	pool, err := storage.NewPool(appCtx, bundle.Env.DatabaseURL)
	if err != nil {
		logger.Error("failed to connect database", "err_code", "DB_CONNECT_ERROR")
		os.Exit(1)
	}
	defer pool.Close()

	// Ensure bootstrap user, settings, and default garden exist idempotently
	bootstrapUserID, err := storage.EnsureBootstrap(
		appCtx,
		pool,
		bundle.Env.DefaultTimezone,
		bundle.Env.GardenDefaultCols,
		bundle.Env.GardenDefaultRows,
		bundle.Models.DefaultReleaseMode,
		bundle.Models.DefaultGrowMode,
	)
	if err != nil {
		logger.Error("bootstrap ensure failed (did you run cmd/migrate up?)", "err_code", "BOOTSTRAP_ERROR")
		os.Exit(1)
	}

	resolver := identity.NewSingleUserResolver(bootstrapUserID)
	broker := ai.NewSSEBroker()

	providers := map[string]ai.ChatProvider{
		"openai":      openai.NewAdapter(bundle.Env.OpenAIAPIKey, bundle.Env.OpenAIBaseURL, nil),
		"gemini":      gemini.NewAdapter(bundle.Env.GeminiAPIKey, bundle.Env.GeminiBaseURL, nil),
		"mock_openai": mock.NewAdapter("mock_openai", 40*time.Millisecond, nil),
		"mock_gemini": mock.NewAdapter("mock_gemini", 50*time.Millisecond, nil),
	}

	worker := ai.NewWorker(pool, bundle.Models, bundle.Prompts, providers, broker, ai.WorkerConfig{
		Concurrency:      bundle.Env.AIWorkerConcurrency,
		RunTimeout:       bundle.Env.AIRunTimeout,
		FirstTextTimeout: bundle.Env.AIFirstTextTimeout,
		IdleTimeout:      bundle.Env.AIIdleTimeout,
	}, logger)
	workerWG := worker.Start(appCtx)

	cleaner := storage.NewCleaner(pool, bundle.Env.UsageRetention, logger)
	cleaner.Start(appCtx, 10*time.Minute)

	charSvc := character.NewService(pool, bundle.Avatars, bundle.Env.IdempotencyRetention)
	ritualSvc := ritual.NewService(pool, bundle.Rituals, bundle.Flowers, broker, bundle.Env.IdempotencyRetention)
	gardenSvc := garden.NewService(pool, bundle.Rituals, ritualSvc, bundle.Env.GardenMaxCols, bundle.Env.GardenMaxRows, bundle.Env.IdempotencyRetention)
	convSvc := conversation.NewService(pool, bundle, charSvc, ritualSvc, worker, broker, providers)

	srvHandler := httpapi.NewServer(pool, bundle, resolver, charSvc, convSvc, ritualSvc, gardenSvc, broker)

	httpServer := &http.Server{
		Addr:              bundle.Env.HTTPAddr,
		Handler:           srvHandler.Router(httpapi.RequestContextAndSafeLogger(logger)),
		ReadHeaderTimeout: 10 * time.Second,
		ReadTimeout:       30 * time.Second,
		// WriteTimeout is intentionally 0 so SSE streams are not prematurely killed;
		// individual worker/DB operations carry explicit context timeouts (Spec Section 26.2).
		IdleTimeout: 120 * time.Second,
	}

	go func() {
		logger.Info("server listening", "addr", bundle.Env.HTTPAddr, "auth_mode", bundle.Env.AuthMode)
		if err := httpServer.ListenAndServe(); err != nil && !errors.Is(err, http.ErrServerClosed) {
			logger.Error("http server error", "err_code", "HTTP_LISTEN_ERROR")
			stopSignals()
		}
	}()

	<-appCtx.Done()
	logger.Info("shutdown signal received; draining HTTP & AI workers")

	shutCtx, cancelShut := context.WithTimeout(context.Background(), 15*time.Second)
	defer cancelShut()
	_ = httpServer.Shutdown(shutCtx)

	doneCh := make(chan struct{})
	go func() {
		workerWG.Wait()
		close(doneCh)
	}()
	select {
	case <-doneCh:
		logger.Info("graceful shutdown complete")
	case <-time.After(10 * time.Second):
		logger.Warn("worker drain timed out")
	}
}
