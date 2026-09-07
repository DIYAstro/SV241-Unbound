// Package profiles implements user-named, manually-saved configuration snapshots ("Profiles") -
// the proxy-side equivalent of the handful of on-device profiles some other SV241 firmwares (e.g.
// piwi3910/SV241-indi) support, but stored in the proxy's own config directory instead of the
// ESP32's flash, so the count isn't limited to a fixed number of slots.
//
// A Profile deliberately stores only what belongs to *one* box: its firmware config plus that one
// box's own config.DeviceProfile entry (rig name, switch names, ...) - never the full
// config.ProxyConfig/DeviceProfiles map (that's what a Backup is for, see internal/backup).
// Storing the whole map here was tried first and found to be a real bug: applying such a profile
// went through applyBackupRestore(), which replaces the *entire* DeviceProfiles map wholesale -
// silently deleting every other known box's names. Profiles are stored in their own
// <config dir>/profiles/ directory rather than the automatic backups/ one - the two features
// never interfere (different directory, different filename prefix, no shared retention/pruning
// logic; internal/backup is never modified by this package, only called into for the actual
// firmware config capture).
package profiles

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"

	"sv241pro-alpaca-proxy/internal/backup"
	"sv241pro-alpaca-proxy/internal/config"
)

// FilePrefix/FileSuffix/TimestampForm identify and parse profile filenames
// (sv241_profile_20060102_150405.json) - mirrors backup.FilePrefix/FileSuffix/TimestampForm so
// the same "the timestamp in the filename sorts lexically in the same order as chronologically"
// trick applies here too.
const (
	FilePrefix    = "sv241_profile_"
	FileSuffix    = ".json"
	TimestampForm = "20060102_150405"
)

// Profile is a named, manually-saved snapshot of one box's configuration - its firmware config
// plus that one box's own config.DeviceProfile (rig name, switch names, lens temp name, heater
// auto-enable-leader, weather source priority). The Name is stored inside the file content, never
// in the filename itself, so it can contain any characters at all (spaces, umlauts, emoji, ...)
// without needing to be filesystem-safe.
type Profile struct {
	Name           string               `json:"name"`
	SavedAt        time.Time            `json:"savedAt"`
	DeviceSerial   string               `json:"deviceSerial"`
	FirmwareConfig json.RawMessage      `json:"firmwareConfig"`
	DeviceProfile  config.DeviceProfile `json:"deviceProfile"`
}

// Entry is the lightweight metadata surfaced by ListProfiles/SaveProfile/UpdateProfile for
// building UI list rows and API responses, without the caller needing to unmarshal the (possibly
// large) embedded FirmwareConfig every time. RigName is taken from the Profile's own
// DeviceProfile as it was at save time, not looked up live - it should reflect what the profile
// itself calls the rig, not whatever that device might have been renamed to since.
type Entry struct {
	Filename     string `json:"filename"`
	Name         string `json:"name"`
	SavedAt      string `json:"savedAt"` // RFC3339
	DeviceSerial string `json:"deviceSerial"`
	RigName      string `json:"rigName"`
}

func dir() string {
	return filepath.Join(config.GetConfigDir(), "profiles")
}

func entryFromProfile(filename string, p *Profile) Entry {
	return Entry{
		Filename:     filename,
		Name:         p.Name,
		SavedAt:      p.SavedAt.Format(time.RFC3339),
		DeviceSerial: p.DeviceSerial,
		RigName:      p.DeviceProfile.RigName,
	}
}

// captureCurrent builds a Profile from the live configuration - the firmware config plus only the
// currently active device's own DeviceProfile entry (see the package doc comment for why not the
// whole map). Shared by SaveProfile and UpdateProfile so both capture identically.
func captureCurrent(name string) (*Profile, error) {
	snap, err := backup.BuildSnapshot()
	if err != nil {
		return nil, err
	}
	if snap.FirmwareConfigSerial == "" {
		return nil, fmt.Errorf("no device connected - can't tell which box's settings to save")
	}
	return &Profile{
		Name:           name,
		SavedAt:        time.Now(),
		DeviceSerial:   snap.FirmwareConfigSerial,
		FirmwareConfig: snap.FirmwareConfig,
		DeviceProfile:  config.GetDeviceProfile(snap.FirmwareConfigSerial),
	}, nil
}

func writeProfile(filename string, p *Profile) error {
	if err := os.MkdirAll(dir(), 0o755); err != nil {
		return fmt.Errorf("could not create profiles directory: %w", err)
	}
	data, err := json.MarshalIndent(p, "", "  ")
	if err != nil {
		return fmt.Errorf("could not serialize profile: %w", err)
	}
	if err := os.WriteFile(filepath.Join(dir(), filename), data, 0o644); err != nil {
		return fmt.Errorf("could not write profile file: %w", err)
	}
	return nil
}

// validFilename reports whether filename exactly matches an existing entry in the profiles
// directory - the path-traversal guard for every lookup below, mirroring the same check
// server.go's handleRestoreAutoBackup does for automatic backups (a real directory entry's
// Name() never contains "/", "\", or "..", so an exact match against the actual listing is
// sufficient - filename comes from an HTTP query parameter and must never be joined onto dir()
// unchecked).
func validFilename(filename string) bool {
	entries, err := os.ReadDir(dir())
	if err != nil {
		return false
	}
	for _, e := range entries {
		if !e.IsDir() && e.Name() == filename {
			return true
		}
	}
	return false
}

// SaveProfile captures the current live configuration (firmware config + the active device's own
// DeviceProfile) and stores it under a new, timestamp-named file tagged with name. Purely manual -
// unlike internal/backup, nothing ever calls this automatically.
func SaveProfile(name string) (*Entry, error) {
	profile, err := captureCurrent(name)
	if err != nil {
		return nil, err
	}
	filename := fmt.Sprintf("%s%s%s", FilePrefix, time.Now().Format(TimestampForm), FileSuffix)
	if err := writeProfile(filename, profile); err != nil {
		return nil, err
	}
	entry := entryFromProfile(filename, profile)
	return &entry, nil
}

// ListProfiles returns metadata for every saved profile, newest first. A missing profiles
// directory (e.g. before the first profile is ever saved) is not an error - just an empty list,
// same convention as handleListAutoBackups uses for the backups directory.
func ListProfiles() ([]Entry, error) {
	entries, err := os.ReadDir(dir())
	if err != nil {
		return []Entry{}, nil
	}
	list := make([]Entry, 0, len(entries))
	for _, e := range entries {
		if e.IsDir() || !strings.HasPrefix(e.Name(), FilePrefix) || !strings.HasSuffix(e.Name(), FileSuffix) {
			continue
		}
		data, err := os.ReadFile(filepath.Join(dir(), e.Name()))
		if err != nil {
			continue // best-effort, same as handleListAutoBackups's peek
		}
		var p Profile
		if err := json.Unmarshal(data, &p); err != nil {
			continue
		}
		list = append(list, entryFromProfile(e.Name(), &p))
	}
	// Filenames sort lexically in the same order as their embedded timestamp - newest first.
	sort.Slice(list, func(i, j int) bool { return list[i].Filename > list[j].Filename })
	return list, nil
}

// LoadProfile returns the full profile for filename - e.g. to apply it.
func LoadProfile(filename string) (*Profile, error) {
	if !validFilename(filename) {
		return nil, fmt.Errorf("profile file not found: %s", filename)
	}
	data, err := os.ReadFile(filepath.Join(dir(), filename))
	if err != nil {
		return nil, fmt.Errorf("could not read profile file: %w", err)
	}
	var p Profile
	if err := json.Unmarshal(data, &p); err != nil {
		return nil, fmt.Errorf("invalid profile file format: %w", err)
	}
	return &p, nil
}

// UpdateProfile re-captures the current live configuration and overwrites the existing profile
// file with it, keeping its filename and Name unchanged (only SavedAt/DeviceSerial/
// FirmwareConfig/DeviceProfile move forward) - lets a user bring an existing profile in line with
// settings they've since tweaked, without deleting and re-saving under a new file/name.
func UpdateProfile(filename string) (*Entry, error) {
	existing, err := LoadProfile(filename)
	if err != nil {
		return nil, err
	}
	updated, err := captureCurrent(existing.Name)
	if err != nil {
		return nil, err
	}
	if err := writeProfile(filename, updated); err != nil {
		return nil, err
	}
	entry := entryFromProfile(filename, updated)
	return &entry, nil
}

// DeleteProfile permanently removes filename from the profiles directory.
func DeleteProfile(filename string) error {
	if !validFilename(filename) {
		return fmt.Errorf("profile file not found: %s", filename)
	}
	return os.Remove(filepath.Join(dir(), filename))
}
