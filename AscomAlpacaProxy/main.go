package main

import (
	"embed"
	"io/fs"
	"sv241pro-alpaca-proxy/internal/alpaca"
	"sv241pro-alpaca-proxy/internal/config"
	"sv241pro-alpaca-proxy/internal/events"
	"sv241pro-alpaca-proxy/internal/logger"
	"sv241pro-alpaca-proxy/internal/logstream"
	"sv241pro-alpaca-proxy/internal/serial"
	"sv241pro-alpaca-proxy/internal/server"
	"sv241pro-alpaca-proxy/internal/systray"
)

//go:embed icon.ico
var iconData []byte

//go:embed frontend-vue/dist
var embeddedFS embed.FS

var frontendFS fs.FS

// AppVersion wird zur Build-Zeit durch ldflags gesetzt.
// Der Standardwert "dev" wird verwendet, wenn das Programm ohne die ldflags kompiliert wird (z.B. bei `go run`).
var AppVersion string = "dev"

func main() {
	var err error
	frontendFS, err = fs.Sub(embeddedFS, "frontend-vue/dist")
	if err != nil {
		// This is a critical error at startup. A message box is appropriate.
		systray.ShowMessageBox("Fatal Error", "Failed to load embedded frontend files. The application will exit.", 0x10)
		return
	}

	// Systray.Run is blocking and will handle the application lifecycle.
	// It calls startApp from its OnReady callback.
	systray.Run(startApp, iconData)
}

// startApp initializes and starts all the application's components.
func startApp() {
	// 1. Start the WebSocket hub for live logging.
	logStreamHub := logstream.NewHub()
	go logStreamHub.Run()

	// 2. Initialize the logger to use the hub as a writer.
	if err := logger.Setup(&logstream.Broadcaster{}); err != nil {
		// If logger fails, we can't do much else.
		// A message box might be appropriate for GUI mode.
		systray.ShowMessageBox("Fatal Error", "Failed to initialize file logger. The application will exit.", 0x10)
		return
	}

	// 3. Load the proxy configuration.
	if err := config.Load(); err != nil {
		logger.Fatal("Failed to load proxy configuration: %v", err)
	}

	// 4. Start background tasks for serial communication and cache updates.
	// This will perform the initial connection attempt.
	// 4. Start background tasks for serial communication and cache updates.
	// This will perform the initial connection attempt.
	serial.StartManager()

	// Ensure the systray listener is ready. This call is safe to make here.
	events.StartListener(func() {}) // This just ensures the 'once.Do' is triggered if it hasn't been already.

	// 5. Start the Alpaca discovery responder.
	go alpaca.RespondToDiscovery()

	// Fetch firmware version in the background after initialization is complete.
	go serial.FetchFirmwareVersion()

	// 6. Start the web server. This is a blocking call and will run for the
	// lifetime of the application, so it must be last.
	server.Start(frontendFS, AppVersion)
}
