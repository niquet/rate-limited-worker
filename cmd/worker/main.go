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

	"github.com/niquet/rate-limited-worker/internal/config"
	"github.com/niquet/rate-limited-worker/internal/handlers"
	"github.com/niquet/rate-limited-worker/internal/middleware"
	"github.com/niquet/rate-limited-worker/internal/service"
	"github.com/niquet/rate-limited-worker/internal/telemetry"

	"go.opentelemetry.io/contrib/instrumentation/net/http/otelhttp"
	"go.opentelemetry.io/otel"
	"go.opentelemetry.io/otel/attribute"
)

const (
	serviceName = "worker"
	version     = "v1.0.0"
)

func main() {
	// Load configuration
	cfg, err := config.Load()
	if err != nil {
		slog.Error("Failed to load configuration", "error", err)
		os.Exit(1)
	}

	// Setup structured logging
	setupLogging(cfg.LogLevel)

	// Initialize OpenTelemetry
	shutdown, err := telemetry.SetupOTelSDK(context.Background(), serviceName, version, cfg.OTELEndpoint)
	if err != nil {
		slog.Error("Failed to setup OpenTelemetry", "error", err)
		os.Exit(1)
	}
	defer func() {
		if err := shutdown(context.Background()); err != nil {
			slog.Error("Failed to shutdown OpenTelemetry", "error", err)
		}
	}()

	// Initialize service layer
	svc := service.New()

	// Setup HTTP handlers with middleware
	mux := http.NewServeMux()

	// Wrap handlers with OTEL and custom middleware
	handler := handlers.New(svc)

	// Static files
	fs := http.FileServer(http.Dir("./web/static/"))
	mux.Handle("/static/", middleware.Chain(
		http.StripPrefix("/static/", fs),
		middleware.RequestLogger(),
		middleware.CORS(),
	))

	// Main page
	mux.Handle("/", middleware.Chain(
		http.HandlerFunc(handler.HomePage),
		middleware.RequestLogger(),
		middleware.CORS(),
		middleware.MetricsCollector(svc),
	))

	// API endpoints
	mux.Handle("/api/track", middleware.Chain(
		http.HandlerFunc(handler.TrackEvent),
		middleware.RequestLogger(),
		middleware.CORS(),
		middleware.MetricsCollector(svc),
	))

	mux.Handle("/api/health", middleware.Chain(
		http.HandlerFunc(handler.HealthCheck),
		middleware.RequestLogger(),
	))

	// Wrap the entire mux with OTEL HTTP instrumentation
	otelHandler := otelhttp.NewHandler(mux, "worker-server",
		otelhttp.WithServerName(serviceName),
	)

	// Create HTTP server
	server := &http.Server{
		Addr:         fmt.Sprintf(":%d", cfg.Port),
		Handler:      otelHandler,
		ReadTimeout:  30 * time.Second,
		WriteTimeout: 30 * time.Second,
		IdleTimeout:  120 * time.Second,
	}

	// Add server attributes to traces
	tracer := otel.Tracer(serviceName)
	_, span := tracer.Start(context.Background(), "server_startup")
	span.SetAttributes(
		attribute.String("server.port", fmt.Sprintf("%d", cfg.Port)),
		attribute.String("server.version", version),
	)
	span.End()

	// Start server in goroutine
	serverErr := make(chan error, 1)
	go func() {
		slog.Info("Starting HTTP server",
			"port", cfg.Port,
			"service", serviceName,
			"version", version)

		if err := server.ListenAndServe(); err != nil && !errors.Is(err, http.ErrServerClosed) {
			serverErr <- err
		}
	}()

	// Wait for shutdown signal
	quit := make(chan os.Signal, 1)
	signal.Notify(quit, syscall.SIGINT, syscall.SIGTERM)

	select {
	case err := <-serverErr:
		slog.Error("Server failed to start", "error", err)
	case sig := <-quit:
		slog.Info("Received shutdown signal", "signal", sig.String())
	}

	// Graceful shutdown
	slog.Info("Server shutting down...")
	shutdownCtx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	if err := server.Shutdown(shutdownCtx); err != nil {
		slog.Error("Server forced to shutdown", "error", err)
	}

	slog.Info("Server exited")
}

func setupLogging(level string) {
	var logLevel slog.Level
	switch level {
	case "DEBUG":
		logLevel = slog.LevelDebug
	case "INFO":
		logLevel = slog.LevelInfo
	case "WARN":
		logLevel = slog.LevelWarn
	case "ERROR":
		logLevel = slog.LevelError
	default:
		logLevel = slog.LevelInfo
	}

	opts := &slog.HandlerOptions{
		Level: logLevel,
	}

	handler := slog.NewJSONHandler(os.Stdout, opts)
	logger := slog.New(handler)
	slog.SetDefault(logger)
}
