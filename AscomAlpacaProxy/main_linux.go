//go:build linux

package main

import (
	"fmt"
	"io/fs"
	"os"
	"os/signal"
	"sv241pro-alpaca-proxy/internal/logger"
	"syscall"
)

func main() {
	var err error
	frontendFS, err = fs.Sub(embeddedFS, "frontend-vue/dist")
	if err != nil {
		fmt.Fprintf(os.Stderr, "FATAL: Failed to load embedded frontend files: %v\n", err)
		os.Exit(1)
	}

	// Handle graceful shutdown via SIGINT (Ctrl+C) and SIGTERM (systemctl stop).
	sigChan := make(chan os.Signal, 1)
	signal.Notify(sigChan, syscall.SIGINT, syscall.SIGTERM)

	go func() {
		sig := <-sigChan
		fmt.Printf("\nReceived signal %v. Shutting down...\n", sig)
		logger.Close()
		os.Exit(0)
	}()

	fmt.Println("SV241 Alpaca Proxy starting...")
	fmt.Printf("Version: %s\n", AppVersion)

	// Run directly as a CLI process (blocking).
	startApp()
}
