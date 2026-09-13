package main

import (
	"embed"
	"fmt"
	"io/fs"
	"os"
	"sv241pro-alpaca-proxy/internal/alpaca"
	"sv241pro-alpaca-proxy/internal/backup"
	"sv241pro-alpaca-proxy/internal/config"
	"sv241pro-alpaca-proxy/internal/flasher"
	"sv241pro-alpaca-proxy/internal/logger"
	"sv241pro-alpaca-proxy/internal/logstream"
	"sv241pro-alpaca-proxy/internal/serial"
	"sv241pro-alpaca-proxy/internal/server"
	"sv241pro-alpaca-proxy/internal/weather"
)

//go:embed frontend-vue/dist
var embeddedFS embed.FS

var frontendFS fs.FS

// releaseVersionJSON is release_version.json, the checked-in single source of truth for both
// version numbers (see that file's own comment) - embedded directly rather than relying on
// firmware/version.json under frontend-vue/dist/flasher (only written by the full
// build_scripts/build_exe.bat or build_linux.sh as a post-build step, and so absent from a plain
// `go build`/`vite build`, which previously made internal/flasher.GetInfo report the bundled
// firmware version as "unknown" for any non-release build). Embedding this file instead works
// unconditionally: go:embed reads it straight from the module tree at compile time, with no
// build-script step required.
//
//go:embed build_scripts/release_version.json
var releaseVersionJSON []byte

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

	// 5. Start the Alpaca discovery responder.
	go alpaca.RespondToDiscovery()

	// Fetch firmware version in the background after initialization is complete.
	go serial.FetchFirmwareVersion()

	// 6. Start the weather service poller.
	weather.GetService().Start()

	// 6a. Wire the native firmware flasher (internal/flasher) to the same embedded frontend
	// filesystem server.Start below uses (for the bundled bootloader/partitions/firmware .bin
	// bytes) and to release_version.json (for the bundled version number - see that var's own
	// comment for why it's embedded separately rather than read from the frontend dist tree).
	flasher.Init(frontendFS, releaseVersionJSON)

	// 7. Start the web server. This is a blocking call and will run for the
	// lifetime of the application, so it must be last.
	server.Start(frontendFS, AppVersion)
}
