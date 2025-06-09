package main

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"os"
	"os/signal"
	"syscall"
	"time"

	"slimserve/internal/config"
	"slimserve/internal/logger"
	"slimserve/internal/server"

	"github.com/rs/zerolog/log"
)

func main() {
	if err := Run(context.Background()); err != nil && !errors.Is(err, http.ErrServerClosed) {
		log.Fatal().Err(err).Msg("failed to run server")
	}
}

func Run(ctx context.Context) error {
	cfg, err := config.Load()
	if err != nil {
		return fmt.Errorf("failed to load configuration: %w", err)
	}

	if len(cfg.Directories) == 0 {
		cfg.Directories = []string{"."}
	}

	if err := logger.Init(cfg); err != nil {
		return fmt.Errorf("failed to initialize logger: %w", err)
	}

	srv := server.New(cfg)
	addr := fmt.Sprintf("%s:%d", cfg.Host, cfg.Port)
	logger.Infof("Starting SlimServe on %s, serving directories: %v", addr, cfg.Directories)

	serverErr := make(chan error, 1)
	go func() {
		serverErr <- srv.Run(addr)
	}()

	shutdownCtx, stop := signal.NotifyContext(ctx, os.Interrupt, syscall.SIGTERM)
	defer stop()

	select {
	case err := <-serverErr:
		if err != nil && !errors.Is(err, http.ErrServerClosed) {
			return fmt.Errorf("server error: %w", err)
		}
		return nil
	case <-shutdownCtx.Done():
		log.Info().Msg("Shutting down server...")
		shutdownTimeoutCtx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()

		if err := srv.Shutdown(shutdownTimeoutCtx); err != nil {
			return fmt.Errorf("server shutdown failed: %w", err)
		}
		log.Info().Msg("Server gracefully stopped")
		return nil
	}
}
