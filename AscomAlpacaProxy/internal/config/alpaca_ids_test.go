package config

import (
	"encoding/json"
	"os"
	"path/filepath"
	"testing"

	"github.com/google/uuid"
)

// withTempConfigFile points the package at a scratch proxy_config.json (optionally pre-filled)
// and restores the real path/singleton afterwards. doLoad is called directly, bypassing Load's
// sync.Once, so each test can simulate a fresh process start.
func withTempConfigFile(t *testing.T, contents string) string {
	t.Helper()
	path := filepath.Join(t.TempDir(), "proxy_config.json")
	if contents != "" {
		if err := os.WriteFile(path, []byte(contents), 0644); err != nil {
			t.Fatal(err)
		}
	}
	origFile, origConf := proxyConfigFile, proxyConfig
	proxyConfigFile, proxyConfig = path, nil
	t.Cleanup(func() { proxyConfigFile, proxyConfig = origFile, origConf })
	return path
}

func savedIDs(t *testing.T, path string) (AlpacaDeviceIDs, bool) {
	t.Helper()
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	var raw map[string]json.RawMessage
	if err := json.Unmarshal(data, &raw); err != nil {
		t.Fatal(err)
	}
	v, ok := raw["alpacaUniqueIds"]
	if !ok {
		return AlpacaDeviceIDs{}, false
	}
	var ids AlpacaDeviceIDs
	if err := json.Unmarshal(v, &ids); err != nil {
		t.Fatal(err)
	}
	return ids, true
}

func assertFreshGUIDs(t *testing.T, ids AlpacaDeviceIDs) {
	t.Helper()
	all := []string{ids.Switch, ids.ObservingConditions, ids.SafetyMonitor}
	seen := map[string]bool{}
	for _, id := range all {
		if _, err := uuid.Parse(id); err != nil {
			t.Errorf("%q is not a valid GUID: %v", id, err)
		}
		if seen[id] {
			t.Errorf("duplicate ID %q across device types", id)
		}
		seen[id] = true
	}
	if ids.IsLegacy() {
		t.Error("fresh IDs must not be the legacy shared IDs")
	}
}

func TestNewInstallGetsRandomPersistentIDs(t *testing.T) {
	path := withTempConfigFile(t, "")
	if err := doLoad(); err != nil {
		t.Fatal(err)
	}
	first := proxyConfig.AlpacaUniqueIDs
	assertFreshGUIDs(t, first)

	if saved, ok := savedIDs(t, path); !ok || saved != first {
		t.Fatalf("IDs not persisted on first run: saved=%+v ok=%v want=%+v", saved, ok, first)
	}

	proxyConfig = nil // simulate restart
	if err := doLoad(); err != nil {
		t.Fatal(err)
	}
	if proxyConfig.AlpacaUniqueIDs != first {
		t.Fatalf("IDs changed across restart: got %+v want %+v", proxyConfig.AlpacaUniqueIDs, first)
	}
}

func TestUpgradedConfigKeepsLegacyIDs(t *testing.T) {
	// A config written by a pre-alpacaUniqueIds proxy.
	path := withTempConfigFile(t, `{"networkPort":32241,"listenAddress":"0.0.0.0","logLevel":"INFO"}`)
	if err := doLoad(); err != nil {
		t.Fatal(err)
	}
	if proxyConfig.AlpacaUniqueIDs != legacyAlpacaUniqueIDs {
		t.Fatalf("upgraded config should keep legacy IDs, got %+v", proxyConfig.AlpacaUniqueIDs)
	}
	if saved, ok := savedIDs(t, path); !ok || saved != legacyAlpacaUniqueIDs {
		t.Fatalf("legacy IDs not persisted: saved=%+v ok=%v", saved, ok)
	}
}

func TestBlankEntryIsRegeneratedOthersKept(t *testing.T) {
	path := withTempConfigFile(t, `{"networkPort":32241,"listenAddress":"0.0.0.0","logLevel":"INFO",
		"alpacaUniqueIds":{"switch":"keep-me","observingConditions":"","safetyMonitor":"  "}}`)
	if err := doLoad(); err != nil {
		t.Fatal(err)
	}
	ids := proxyConfig.AlpacaUniqueIDs
	if ids.Switch != "keep-me" {
		t.Errorf("non-blank ID was replaced: %q", ids.Switch)
	}
	for _, id := range []string{ids.ObservingConditions, ids.SafetyMonitor} {
		if _, err := uuid.Parse(id); err != nil {
			t.Errorf("blank entry not filled with a GUID: %q", id)
		}
	}
	if saved, _ := savedIDs(t, path); saved != ids {
		t.Fatalf("filled IDs not persisted: saved=%+v want=%+v", saved, ids)
	}
}

func TestRegenerateReplacesLegacyIDsAndPersists(t *testing.T) {
	path := withTempConfigFile(t, `{"networkPort":32241,"listenAddress":"0.0.0.0","logLevel":"INFO"}`)
	if err := doLoad(); err != nil {
		t.Fatal(err)
	}
	ids, err := RegenerateAlpacaUniqueIDs()
	if err != nil {
		t.Fatal(err)
	}
	assertFreshGUIDs(t, ids)
	if GetAlpacaUniqueIDs() != ids {
		t.Fatal("in-memory IDs don't match the regenerated ones")
	}
	if saved, _ := savedIDs(t, path); saved != ids {
		t.Fatalf("regenerated IDs not persisted: saved=%+v want=%+v", saved, ids)
	}

	proxyConfig = nil // a restart must not bring the legacy IDs back
	if err := doLoad(); err != nil {
		t.Fatal(err)
	}
	if proxyConfig.AlpacaUniqueIDs != ids {
		t.Fatalf("regenerated IDs lost on restart: got %+v want %+v", proxyConfig.AlpacaUniqueIDs, ids)
	}
}
