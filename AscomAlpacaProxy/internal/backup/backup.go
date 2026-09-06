// Package backup implements automatic configuration backups: a full snapshot (proxy config +
// live firmware config, the same shape the manual "Download Backup" button produces) written to
// <config dir>/backups/ every time the SV241 connects, plus a daily safety-net check for a box
// that stays connected for days without ever reconnecting.
package backup

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"sync"
	"time"

	"sv241pro-alpaca-proxy/internal/config"
	"sv241pro-alpaca-proxy/internal/logger"
	"sv241pro-alpaca-proxy/internal/serial"
)

// FilePrefix/FileSuffix/TimestampForm identify and parse automatic backup filenames
// (sv241_backup_20060102_150405.json) - exported so server.go's list/restore-auto handlers can
// recognize and parse the same files without duplicating the format.
const (
	FilePrefix    = "sv241_backup_"
	FileSuffix    = ".json"
	TimestampForm = "20060102_150405"
)

// BuildSnapshot fetches the live firmware config over serial and combines it with the current
// proxy config into the same CombinedConfig shape the manual backup/restore endpoints use. Shared
// by the manual "Download Backup" handler and the automatic backup writer below, so both stay
// byte-for-byte identical in structure - a file produced by either can be restored via either.
func BuildSnapshot() (*config.CombinedConfig, error) {
	firmwareConfigJSON, err := serial.SendCommand(`{"get":"config"}`, true, 0)
	if err != nil {
		return nil, err
	}
	return &config.CombinedConfig{
		ProxyConfig:          config.Get(),
		FirmwareConfig:       json.RawMessage(firmwareConfigJSON),
		FirmwareConfigSerial: config.GetActiveDeviceSerial(),
	}, nil
}

var (
	lastBackupTime time.Time
	mu             sync.Mutex // guards lastBackupTime; touched from the connect hook and the daily timer
)

// OnConnected is wired to serial.OnDeviceConnected - called on every fresh connection (initial
// connect and every later reconnect, same box or a different one). Always backs up
// unconditionally when enabled; no gating here on purpose - a connection event is itself the
// meaningful moment (e.g. swapping boxes, or a box coming back after being unplugged, should
// always get its own fresh backup).
func OnConnected() {
	conf := config.Get()
	if !conf.EnableAutoBackup {
		return
	}
	if writeBackup(conf.AutoBackupRetentionCount) {
		mu.Lock()
		lastBackupTime = time.Now()
		mu.Unlock()
	}
}

// RunDailySafetyNet re-checks once an hour for as long as the process runs, so a box that stays
// connected for days without ever reconnecting still gets a fresh backup every 24h - OnConnected
// alone would only ever fire once for such a box.
func RunDailySafetyNet() {
	for {
		time.Sleep(1 * time.Hour)
		runDailySafetyNetCheck()
	}
}

// runDailySafetyNetCheck only writes a backup if >=24h have passed since the last one (connect-
// or timer-triggered).
func runDailySafetyNetCheck() {
	conf := config.Get()
	if !conf.EnableAutoBackup || config.GetActiveDeviceSerial() == "" {
		return
	}
	mu.Lock()
	due := time.Since(lastBackupTime) >= 24*time.Hour
	mu.Unlock()
	if !due {
		return
	}
	if writeBackup(conf.AutoBackupRetentionCount) {
		mu.Lock()
		lastBackupTime = time.Now()
		mu.Unlock()
	}
}

// writeBackup builds one snapshot, writes it to disk, and prunes old ones. Returns true on
// success. Never returns an error to its caller - a failed automatic backup (no device connected,
// disk full, etc.) is only ever logged, never fatal to the running application.
func writeBackup(keep int) bool {
	snap, err := BuildSnapshot()
	if err != nil {
		logger.Warn("Auto-backup skipped: %v", err)
		return false
	}
	dir := filepath.Join(config.GetConfigDir(), "backups")
	if err := os.MkdirAll(dir, 0o755); err != nil {
		logger.Error("Auto-backup: could not create backups directory: %v", err)
		return false
	}
	data, err := json.MarshalIndent(snap, "", "  ")
	if err != nil {
		logger.Error("Auto-backup: could not serialize snapshot: %v", err)
		return false
	}
	filename := fmt.Sprintf("%s%s%s", FilePrefix, time.Now().Format(TimestampForm), FileSuffix)
	path := filepath.Join(dir, filename)
	if err := os.WriteFile(path, data, 0o644); err != nil {
		logger.Error("Auto-backup: could not write %s: %v", path, err)
		return false
	}
	logger.Info("Automatic backup saved to %s", path)
	pruneOldBackups(dir, keep)
	return true
}

// pruneOldBackups deletes the oldest automatic backups beyond keep. keep<=0 means "never delete"
// (same convention as ProxyConfig.HistoryRetentionNights).
func pruneOldBackups(dir string, keep int) {
	if keep <= 0 {
		return
	}
	entries, err := os.ReadDir(dir)
	if err != nil {
		logger.Warn("Auto-backup: could not list %s for pruning: %v", dir, err)
		return
	}
	var names []string
	for _, e := range entries {
		if !e.IsDir() && strings.HasPrefix(e.Name(), FilePrefix) && strings.HasSuffix(e.Name(), FileSuffix) {
			names = append(names, e.Name())
		}
	}
	if len(names) <= keep {
		return
	}
	// The timestamp in the filename sorts lexically in the same order as chronologically.
	sort.Strings(names)
	for _, name := range names[:len(names)-keep] {
		path := filepath.Join(dir, name)
		if err := os.Remove(path); err != nil {
			logger.Warn("Auto-backup: could not remove old backup %s: %v", path, err)
		} else {
			logger.Info("Auto-backup: pruned old backup %s", path)
		}
	}
}
