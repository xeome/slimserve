package main

import (
	"fmt"
	"log"

	"slimserve/internal/config"
	"slimserve/internal/logger"
	"slimserve/internal/server"
)

func main() {
	cfg, err := config.Load()
	if err != nil {
		log.Fatalf("Failed to load configuration: %v", err)
	}

	// Set default directory if none specified
	if len(cfg.Directories) == 0 {
		cfg.Directories = []string{"."}
	}

	// Initialize logger
	if err := logger.Init(cfg); err != nil {
		log.Fatalf("Failed to initialize logger: %v", err)
	}

	srv := server.New(cfg)

	addr := fmt.Sprintf("%s:%d", cfg.Host, cfg.Port)
	logger.Infof("Starting SlimServe on %s, serving directories: %v", addr, cfg.Directories)

	if err := srv.Run(addr); err != nil {
		logger.Log.Fatal().Err(err).Msg("Failed to start server")
	}
}
