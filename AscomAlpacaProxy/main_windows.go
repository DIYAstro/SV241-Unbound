//go:build windows

package main

import (
	_ "embed"
	"io/fs"
	"sv241pro-alpaca-proxy/internal/systray"
)

//go:embed icon.ico
var iconData []byte

func init() {
	// Override the fatal notification function to use a Windows MessageBox.
	fatalNotify = func(title, message string) {
		systray.ShowMessageBox(title, message, 0x10)
	}
}

func main() {
	var err error
	frontendFS, err = fs.Sub(embeddedFS, "frontend-vue/dist")
	if err != nil {
		fatalNotify("Fatal Error", "Failed to load embedded frontend files. The application will exit.")
		return
	}

	// Systray.Run is blocking and will handle the application lifecycle.
	// It calls startApp from its OnReady callback.
	systray.Run(startApp, iconData)
}
