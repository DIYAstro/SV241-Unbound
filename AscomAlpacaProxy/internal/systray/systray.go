package systray

import (
	"fmt"
	"os"
	"os/exec"
	"runtime"
	"sv241pro-alpaca-proxy/internal/config"
	"sv241pro-alpaca-proxy/internal/logger"
	"syscall"
	"unsafe"

	"fyne.io/systray"
	"golang.org/x/sys/windows"
)

var singleInstanceMutex windows.Handle

// Run is the entry point for the systray functionality.
func Run(onStart func(), iconData []byte) {
	// Check for single instance before doing anything else.
	checkSingleInstance()
	// The `onStart` function will be called by `systray.Run` via `OnReady`.
	systray.Run(func() { OnReady(onStart, iconData) }, OnExit)
}

// OnReady is called when the system tray is ready.
func OnReady(onStart func(), iconData []byte) {
	systray.SetIcon(iconData)
	systray.SetTitle("SV241 Alpaca Proxy")
	systray.SetTooltip("SV241 Alpaca Proxy Driver is running")

	mSetup := systray.AddMenuItem("Open Setup Page", "Open the web setup page")
	systray.AddSeparator()
	mQuit := systray.AddMenuItem("Exit", "Quit the application")

	// Start the main application logic in a goroutine.
	go onStart()

	// Handle menu clicks.
	go func() {
		for {
			select {
			case <-mSetup.ClickedCh:
				port := config.Get().NetworkPort
				openBrowser(fmt.Sprintf("http://localhost:%d/setup", port))
			case <-mQuit.ClickedCh:
				systray.Quit()
				return
			}
		}
	}()
}

// OnExit is called when the application is requested to exit.
func OnExit() {
	logger.Info("Exiting application.")
	logger.Close()

	// Release the single instance mutex.
	if singleInstanceMutex != 0 {
		logger.Info("Releasing single instance mutex.")
		windows.ReleaseMutex(singleInstanceMutex)
		windows.CloseHandle(singleInstanceMutex)
	}
}

// checkSingleInstance ensures only one instance of the application is running.
func checkSingleInstance() {
	mutexName := "SV241AlpacaProxySingleInstanceMutex"
	handle, err := windows.CreateMutex(nil, true, windows.StringToUTF16Ptr(mutexName))
	lastError := windows.GetLastError()

	if err == nil && lastError != windows.ERROR_ALREADY_EXISTS {
		// This is the first instance.
		singleInstanceMutex = handle
		return
	}

	// Another instance is running. Open the setup page and exit.
	port := config.GetNetworkPortForDiscovery()
	url := fmt.Sprintf("http://localhost:%d/setup", port)
	openBrowser(url)

	if handle != 0 {
		windows.CloseHandle(handle)
	}
	os.Exit(0)
}

// openBrowser opens the specified URL in the default browser.
func openBrowser(url string) {
	var err error
	switch runtime.GOOS {
	case "linux":
		err = exec.Command("xdg-open", url).Start()
	case "windows":
		err = exec.Command("rundll32", "url.dll,FileProtocolHandler", url).Start()
	case "darwin":
		err = exec.Command("open", url).Start()
	default:
		err = fmt.Errorf("unsupported platform")
	}
	if err != nil {
		// Use the logger, but since this can be called before the logger is fully set up,
		// it might only go to stdout.
		logger.Error("Failed to open browser: %v", err)
	}
}

// ShowMessageBox displays a Windows message box.
// This is kept in case it's needed for critical errors before the logger is available.
func ShowMessageBox(title, message string, style uint) {
	user32 := syscall.NewLazyDLL("user32.dll")
	messageBoxW := user32.NewProc("MessageBoxW")
	lpText := syscall.StringToUTF16Ptr(message)
	lpCaption := syscall.StringToUTF16Ptr(title)
	messageBoxW.Call(0, uintptr(unsafe.Pointer(lpText)), uintptr(unsafe.Pointer(lpCaption)), uintptr(style))
}
