package config

import (
	"os"
	"path/filepath"
	"testing"
)

func loadDiscoveryPortFrom(t *testing.T, contents string) int {
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
	if err := doLoad(); err != nil {
		t.Fatal(err)
	}
	return proxyConfig.DiscoveryPort
}

func TestDiscoveryPortDefaults(t *testing.T) {
	cases := map[string]struct {
		contents string
		want     int
	}{
		"new install":       {"", DefaultDiscoveryPort},
		"pre-existing file": {`{"networkPort":32241,"listenAddress":"0.0.0.0","logLevel":"INFO"}`, DefaultDiscoveryPort},
		"out of range":      {`{"networkPort":32241,"listenAddress":"0.0.0.0","logLevel":"INFO","discoveryPort":70000}`, DefaultDiscoveryPort},
		"custom port kept":  {`{"networkPort":32241,"listenAddress":"0.0.0.0","logLevel":"INFO","discoveryPort":32300}`, 32300},
	}
	for name, tc := range cases {
		t.Run(name, func(t *testing.T) {
			if got := loadDiscoveryPortFrom(t, tc.contents); got != tc.want {
				t.Fatalf("discoveryPort = %d, want %d", got, tc.want)
			}
		})
	}
}
