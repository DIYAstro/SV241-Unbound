package main

import (
	"embed"
	"fmt"
	"io/fs"
	"os"
	"sv241pro-alpaca-proxy/internal/alpaca"
	"sv241pro-alpaca-proxy/internal/backup"
	"sv241pro-alpaca-proxy/internal/config"
	"sv241pro-alpaca-proxy/internal/events"
	"sv241pro-alpaca-proxy/internal/logger"
	"sv241pro-alpaca-proxy/internal/logstream"
	"sv241pro-alpaca-proxy/internal/serial"
	"sv241pro-alpaca-proxy/internal/server"
	"sv241pro-alpaca-proxy/internal/weather"
)

//go:embed frontend-vue/dist
var embeddedFS embed.FS

var frontendFS fs.FS

// AppVersion is set at build time via ldflags.
// The default "dev" is used when the program is compiled without ldflags (e.g. 'go run').
var AppVersion string = "dev"

// fatalNotify displays a fatal error to the user.
// On Windows, this is overridden to show a MessageBox via the systray package.
// On Linux, it defaults to stderr output.
var fatalNotify = func(title, message string) {
	fmt.Fprintf(os.Stderr, "FATAL: %s: %s\n", title, message)
}

// startApp initializes and starts all the application's components.
func startApp() {
	// 1. Start the WebSocket hub for live logging.
	logStreamHub := logstream.NewHub()
	go logStreamHub.Run()

	// 2. Initialize the logger to use the hub as a writer.
	if err := logger.Setup(&logstream.Broadcaster{}); err != nil {
		fatalNotify("Fatal Error", "Failed to initialize file logger. The application will exit.")
		return
	}

	// 3. Load the proxy configuration.
	if err := config.Load(); err != nil {
		logger.Fatal("Failed to load proxy configuration: %v", err)
	}

	// 3a. Wire up automatic backups before the first connection attempt can happen, so the very
	// first connect (not just later reconnects) triggers one too. See internal/backup.
	serial.OnDeviceConnected = backup.OnConnected
	go backup.RunDailySafetyNet()

	// 4. Start background tasks for serial communication and cache updates.
	// This will perform the initial connection attempt.
	serial.StartManager()

	// Ensure the event listener is ready. This call is safe to make here.
	events.StartListener(func() {}) // This just ensures the 'once.Do' is triggered if it hasn't been already.

	// 5. Start the Alpaca discovery responder.
	go alpaca.RespondToDiscovery()

	// Fetch firmware version in the background after initialization is complete.
	go serial.FetchFirmwareVersion()

	// 6. Start the weather service poller.
	weather.GetService().Start()

	// 7. Start the web server. This is a blocking call and will run for the
	// lifetime of the application, so it must be last.
	server.Start(frontendFS, AppVersion)
}
