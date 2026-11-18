package main

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"net"
	"net/http"
	"os"
	"os/signal"
	"syscall"
	"time"

	"github.com/Sifr-dev/sysmon_daemon/internal/api"
	"github.com/Sifr-dev/sysmon_daemon/internal/telemetry"
)

const (
	defaultListenAddress = ":8080"
	defaultProcRoot      = "/proc"
	readHeaderTimeout    = 5 * time.Second
	requestTimeout       = 10 * time.Second
	idleTimeout          = 60 * time.Second
	shutdownTimeout      = 10 * time.Second
)

func main() {
	logger := slog.New(slog.NewJSONHandler(os.Stdout, nil))
	if err := run(logger); err != nil {
		logger.Error("system telemetry daemon stopped", "error", err)
		os.Exit(1)
	}
}

func run(logger *slog.Logger) error {
	allowedOrigins, err := api.ParseAllowedOrigins(os.Getenv("SYSMON_ALLOWED_ORIGINS"))
	if err != nil {
		return err
	}

	collector := telemetry.NewCollector(environmentOrDefault("SYSMON_PROC_ROOT", defaultProcRoot))
	handler := api.NewHandler(
		collector,
		api.WithAllowedOrigins(allowedOrigins),
		api.WithLogger(logger),
	)

	server := &http.Server{
		Addr:              environmentOrDefault("SYSMON_ADDR", defaultListenAddress),
		Handler:           handler,
		ReadHeaderTimeout: readHeaderTimeout,
		ReadTimeout:       requestTimeout,
		WriteTimeout:      requestTimeout,
		IdleTimeout:       idleTimeout,
		MaxHeaderBytes:    1 << 20,
	}

	listener, err := net.Listen("tcp", server.Addr)
	if err != nil {
		return fmt.Errorf("listen on %s: %w", server.Addr, err)
	}

	shutdownContext, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()

	serverErrors := make(chan error, 1)
	go func() {
		logger.Info("system telemetry daemon listening", "address", listener.Addr().String())
		serverErrors <- server.Serve(listener)
	}()

	select {
	case err := <-serverErrors:
		if errors.Is(err, http.ErrServerClosed) {
			return nil
		}
		return err
	case <-shutdownContext.Done():
		logger.Info("shutting down system telemetry daemon")
	}

	gracefulContext, cancel := context.WithTimeout(context.Background(), shutdownTimeout)
	defer cancel()
	if err := server.Shutdown(gracefulContext); err != nil {
		return err
	}

	err = <-serverErrors
	if errors.Is(err, http.ErrServerClosed) {
		return nil
	}
	return err
}

func environmentOrDefault(name, fallback string) string {
	if value := os.Getenv(name); value != "" {
		return value
	}
	return fallback
}
