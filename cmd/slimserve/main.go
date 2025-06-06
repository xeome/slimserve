package main

import (
	"flag"
	"fmt"
	"log"
	"os"

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

	// If no directories specified, default to current working directory
	if len(dirs) == 0 {
		cwd, err := os.Getwd()
		if err != nil {
			log.Fatal("Failed to get current working directory:", err)
		}
		dirs = append(dirs, cwd)
	}

	srv := server.New([]string(dirs))

	addr := fmt.Sprintf(":%d", *port)
	log.Printf("Starting SlimServe on %s, serving directories: %v", addr, dirs)

	if err := srv.Run(addr); err != nil {
		log.Fatal("Failed to start server:", err)
	}
}
