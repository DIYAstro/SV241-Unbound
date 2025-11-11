package logger

import (
	"fmt"
	"io"
	"log"
	"os"
	"path/filepath"
	"strings"
	"time"
)

// LogLevel type
type LogLevel int

const (
	LevelError LogLevel = iota
	LevelWarn
	LevelInfo
	LevelDebug
)

var (
	logFile         *os.File
	currentLogLevel = LevelInfo // Default log level
)

// Setup initializes the file logger and sets the output writers.
func Setup(broadcaster io.Writer) error {
	configDir, err := os.UserConfigDir()
	if err != nil {
		log.Printf("[ERROR] FATAL: Could not get user config directory: %v", err)
		return fmt.Errorf("could not get user config directory: %w", err)
	}

	appConfigDir := filepath.Join(configDir, "SV241AlpacaProxy")
	if err := os.MkdirAll(appConfigDir, 0755); err != nil {
		log.Printf("FATAL: Could not create application config directory '%s': %v", appConfigDir, err)
		return fmt.Errorf("could not create application config directory: %w", err)
	}

	logFilePath := filepath.Join(appConfigDir, "proxy.log")
	oldLogFilePath := filepath.Join(appConfigDir, "proxy.log.old")

	// Log Rotation
	if _, err := os.Stat(oldLogFilePath); err == nil {
		os.Remove(oldLogFilePath)
	}
	if _, err := os.Stat(logFilePath); err == nil {
		if err := os.Rename(logFilePath, oldLogFilePath); err != nil {
			log.Printf("[WARN] Failed to rotate log file: %v", err)
		}
	}

	logFile, err = os.OpenFile(logFilePath, os.O_APPEND|os.O_CREATE|os.O_WRONLY, 0644)
	if err != nil {
		return fmt.Errorf("could not open log file: %w", err)
	}

	// Create a MultiWriter to output to both the file and the WebSocket broadcaster.
	log.SetOutput(io.MultiWriter(logFile, broadcaster))
	log.SetFlags(log.LstdFlags)

	log.Println("---")
	log.Printf("[INFO] --- Log session started at %s ---", time.Now().Format(time.RFC3339))

	return nil
}

// SetLevelFromString updates the global currentLogLevel based on a string value.
func SetLevelFromString(level string) {
	switch strings.ToUpper(level) {
	case "DEBUG":
		currentLogLevel = LevelDebug
	case "INFO":
		currentLogLevel = LevelInfo
	case "WARN":
		currentLogLevel = LevelWarn
	case "ERROR":
		currentLogLevel = LevelError
	default:
		currentLogLevel = LevelInfo
	}
}

// GetLevel returns the current logging level.
func GetLevel() LogLevel {
	return currentLogLevel
}

// Close closes the log file.
func Close() {
	if logFile != nil {
		log.Println("Closing log file.")
		logFile.Close()
	}
}

// Error logs a message at the ERROR level.
func Error(format string, v ...interface{}) {
	if currentLogLevel >= LevelError {
		log.Printf("[ERROR] "+format, v...)
	}
}

// Warn logs a message at the WARN level.
func Warn(format string, v ...interface{}) {
	if currentLogLevel >= LevelWarn {
		log.Printf("[WARN] "+format, v...)
	}
}

// Info logs a message at the INFO level.
func Info(format string, v ...interface{}) {
	if currentLogLevel >= LevelInfo {
		log.Printf("[INFO] "+format, v...)
	}
}

// Debug logs a message at the DEBUG level.
func Debug(format string, v ...interface{}) {
	if currentLogLevel >= LevelDebug {
		log.Printf("[DEBUG] "+format, v...)
	}
}

// Fatal logs a fatal error and ensures the log file is closed before exiting.
func Fatal(format string, v ...interface{}) {
	// Use fmt.Fprintf to write directly to the file handle, bypassing the log buffer.
	if logFile != nil {
		fmt.Fprintf(logFile, "[FATAL] "+format+"\n", v...)
		logFile.Sync()
		logFile.Close()
	} else {
		// Fallback if logFile is not even open
		log.Printf("[FATAL] "+format, v...)
	}
	os.Exit(1)
}
