package main

import (
	"context"
	"flag"
	"fmt"
	"log/slog"
	"net/http"
	"os"
	"os/signal"
	"syscall"
	"time"

	"github.com/prometheus/client_golang/prometheus/promhttp"
	"github.com/rvbsalgado/fencemaster/pkg/logging"
	"github.com/rvbsalgado/fencemaster/pkg/rancher"
	"github.com/rvbsalgado/fencemaster/pkg/webhook"
	"k8s.io/client-go/dynamic"
	"k8s.io/client-go/rest"
)

func main() {
	var (
		port         int
		metricsPort  int
		logLevel     string
		logFormat    string
		strictMode   bool
		dryRun       bool
		cacheTTLMins int
	)

	flag.IntVar(&port, "port", getEnvInt("PORT", 8080), "Webhook server port")
	flag.IntVar(&metricsPort, "metrics-port", getEnvInt("METRICS_PORT", 9090), "Metrics server port")
	flag.StringVar(&logLevel, "log-level", getEnv("LOG_LEVEL", "info"), "Log level (debug, info, warn, error)")
	flag.StringVar(&logFormat, "log-format", getEnv("LOG_FORMAT", "json"), "Log format (json, text)")
	flag.BoolVar(&strictMode, "strict-mode", getEnvBool("STRICT_MODE", false), "Reject namespace if project not found (default: allow without annotation)")
	flag.BoolVar(&dryRun, "dry-run", getEnvBool("DRY_RUN", false), "Log what would happen without actually patching namespaces")
	flag.IntVar(&cacheTTLMins, "cache-ttl", getEnvInt("CACHE_TTL_MINUTES", 5), "Cache TTL in minutes for cluster/project lookups")
	flag.Parse()

	cacheTTL := time.Duration(cacheTTLMins) * time.Minute
	logger := logging.Setup(logLevel, logFormat)

	logger.Info("Starting fencemaster",
		slog.String("log_level", logLevel),
		slog.String("log_format", logFormat),
		slog.Bool("strict_mode", strictMode),
		slog.Bool("dry_run", dryRun),
		slog.Duration("cache_ttl", cacheTTL),
		slog.Int("metrics_port", metricsPort),
	)

	config, err := rest.InClusterConfig()
	if err != nil {
		logger.Error("Failed to get in-cluster config", slog.String("error", err.Error()))
		os.Exit(1)
	}

	dynamicClient, err := dynamic.NewForConfig(config)
	if err != nil {
		logger.Error("Failed to create dynamic client", slog.String("error", err.Error()))
		os.Exit(1)
	}

	rancherClient := rancher.NewClient(dynamicClient, logger, cacheTTL)
	handler := webhook.NewHandler(rancherClient, logger, strictMode, dryRun)

	// Main webhook server
	mux := http.NewServeMux()
	mux.HandleFunc("/mutate/", handler.HandleMutate)

	// Liveness probe - basic server health
	mux.HandleFunc("/healthz", func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte("ok"))
	})

	// Readiness probe - checks Kubernetes API connectivity and RBAC
	mux.HandleFunc("/readyz", func(w http.ResponseWriter, r *http.Request) {
		ctx, cancel := context.WithTimeout(r.Context(), 5*time.Second)
		defer cancel()

		if err := rancherClient.HealthCheck(ctx); err != nil {
			logger.Error("Readiness check failed", slog.String("error", err.Error()))
			http.Error(w, err.Error(), http.StatusServiceUnavailable)
			return
		}
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte("ok"))
	})

	server := &http.Server{
		Addr:              fmt.Sprintf(":%d", port),
		Handler:           mux,
		ReadHeaderTimeout: 10 * time.Second,
	}

	// Metrics server on separate port
	metricsMux := http.NewServeMux()
	metricsMux.Handle("/metrics", promhttp.Handler())

	metricsServer := &http.Server{
		Addr:              fmt.Sprintf(":%d", metricsPort),
		Handler:           metricsMux,
		ReadHeaderTimeout: 10 * time.Second,
	}

	// Start metrics server
	go func() {
		logger.Info("Metrics server started",
			slog.Int("port", metricsPort),
			slog.String("endpoint", "/metrics"),
		)
		if err := metricsServer.ListenAndServe(); err != nil && err != http.ErrServerClosed {
			logger.Error("Failed to start metrics server", slog.String("error", err.Error()))
		}
	}()

	// Start webhook server
	go func() {
		logger.Info("Webhook server started",
			slog.Int("port", port),
			slog.String("endpoint", "/mutate/{cluster-name}"),
		)
		if err := server.ListenAndServe(); err != nil && err != http.ErrServerClosed {
			logger.Error("Failed to start server", slog.String("error", err.Error()))
			os.Exit(1)
		}
	}()

	sigCh := make(chan os.Signal, 1)
	signal.Notify(sigCh, syscall.SIGINT, syscall.SIGTERM)
	sig := <-sigCh

	logger.Info("Received shutdown signal", slog.String("signal", sig.String()))

	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	if err := server.Shutdown(ctx); err != nil {
		logger.Error("Error shutting down webhook server", slog.String("error", err.Error()))
	}
	if err := metricsServer.Shutdown(ctx); err != nil {
		logger.Error("Error shutting down metrics server", slog.String("error", err.Error()))
	}

	logger.Info("Server stopped")
}

func getEnv(key, defaultValue string) string {
	if value := os.Getenv(key); value != "" {
		return value
	}
	return defaultValue
}

func getEnvBool(key string, defaultValue bool) bool {
	if value := os.Getenv(key); value != "" {
		return value == "true" || value == "1" || value == "yes"
	}
	return defaultValue
}

func getEnvInt(key string, defaultValue int) int {
	if value := os.Getenv(key); value != "" {
		var intVal int
		if _, err := fmt.Sscanf(value, "%d", &intVal); err == nil {
			return intVal
		}
	}
	return defaultValue
}
