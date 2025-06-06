package main

import (
	"flag"
	"fmt"
	"log"

	"slimserve/internal/config"
	"slimserve/internal/logger"
	"slimserve/internal/server"
)

// dirList implements flag.Value for multiple directory arguments
type dirList []string

func (d *dirList) String() string {
	return fmt.Sprintf("%v", *d)
}

func (d *dirList) Set(value string) error {
	*d = append(*d, value)
	return nil
}

func main() {
	var (
		port = flag.Int("port", 8080, "Port to serve on")
		dirs = dirList{}
	)

	flag.Var(&dirs, "dirs", "Directory to serve (can be specified multiple times)")
	flag.Parse()

	cfg := config.Default()
	cfg.Port = *port
	if len(dirs) > 0 {
		cfg.Directories = []string(dirs) // preserve default ["."]
	}

	// Initialize logger
	if err := logger.Init(cfg); err != nil {
		log.Fatalf("Failed to initialize logger: %v", err)
	}

	srv := server.New(cfg)

	addr := fmt.Sprintf(":%d", *port)
	logger.Infof("Starting SlimServe on %s, serving directories: %v", addr, cfg.Directories)

	if err := srv.Run(addr); err != nil {
		logger.Log.Fatal().Err(err).Msg("Failed to start server")
	}
}
