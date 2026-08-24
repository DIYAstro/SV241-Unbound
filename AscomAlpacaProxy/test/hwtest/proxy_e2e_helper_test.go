//go:build hwtest

package hwtest

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"math/rand"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
)

// proxyProcess represents a real Proxy binary running as a subprocess, driven purely over HTTP -
// see the plan's rationale: server.Start() registers routes on the global http.DefaultServeMux
// and config/serial are process-wide singletons, so an in-process second Start() would collide
// with (and risks corrupting) the real installed Proxy's own configuration. Running the actual
// compiled binary as a subprocess is also a more faithful black-box test: the exact code path
// real ASCOM clients and the Vue frontend take, not an in-process testing shortcut.
type proxyProcess struct {
	cmd     *exec.Cmd
	baseURL string
	client  *http.Client
	stdout  *bytes.Buffer
	stderr  *bytes.Buffer
}

// startProxy builds the Proxy binary, launches it against comPort with an isolated config
// directory (APPDATA override - see internal/config/config.go's os.UserConfigDir() usage) and a
// non-default network port, and waits for it to be listening AND connected to the device.
func startProxy(t *testing.T, comPort string) *proxyProcess {
	t.Helper()

	tempAppData := t.TempDir()
	configDir := filepath.Join(tempAppData, "SV241AlpacaProxy")
	require.NoError(t, os.MkdirAll(configDir, 0755))

	networkPort := 32300 + rand.Intn(500) // avoid the real proxy's default 32241, reduce collision risk
	proxyCfg := map[string]interface{}{
		"serialPortName":         comPort,
		"autoDetectPort":         false,
		"networkPort":            networkPort,
		"listenAddress":          "127.0.0.1",
		"logLevel":               "INFO",
		"switchNames":            map[string]string{},
		"heaterAutoEnableLeader": map[string]bool{},
		"historyRetentionNights": 1,
		"telemetryInterval":      60,
		"enableAlpacaDiscovery":  false,
		"enableNotifications":    false,
		"weatherInterval":        60,
		"weatherModel":           "best_match",
		"weatherSourcePriority":  map[string]string{},
		"firstRunComplete":       true,
	}
	cfgBytes, err := json.MarshalIndent(proxyCfg, "", "  ")
	require.NoError(t, err)
	require.NoError(t, os.WriteFile(filepath.Join(configDir, "proxy_config.json"), cfgBytes, 0644))

	// Module root is two levels up from this file (AscomAlpacaProxy/test/hwtest -> AscomAlpacaProxy).
	moduleRoot, err := filepath.Abs(filepath.Join("..", ".."))
	require.NoError(t, err)
	binPath := filepath.Join(t.TempDir(), "sv241proxy_hwtest.exe")
	buildCmd := exec.Command("go", "build", "-o", binPath, ".")
	buildCmd.Dir = moduleRoot
	if out, err := buildCmd.CombinedOutput(); err != nil {
		t.Fatalf("failed to build proxy binary (is frontend-vue/dist built? see README.md): %s\n%s", err, out)
	}

	cmd := exec.Command(binPath)
	cmd.Env = append(os.Environ(), "APPDATA="+tempAppData)
	var outBuf, errBuf bytes.Buffer
	cmd.Stdout = &outBuf
	cmd.Stderr = &errBuf
	require.NoError(t, cmd.Start(), "failed to start proxy subprocess")

	baseURL := fmt.Sprintf("http://127.0.0.1:%d", networkPort)
	client := &http.Client{Timeout: 10 * time.Second}
	p := &proxyProcess{cmd: cmd, baseURL: baseURL, client: client, stdout: &outBuf, stderr: &errBuf}

	t.Cleanup(func() {
		if cmd.Process != nil {
			_ = cmd.Process.Kill()
			_, _ = cmd.Process.Wait()
		}
	})

	waitReady(t, p)
	waitDeviceConnected(t, p)
	return p
}

func waitReady(t *testing.T, p *proxyProcess) {
	t.Helper()
	deadline := time.Now().Add(20 * time.Second)
	for time.Now().Before(deadline) {
		resp, err := p.client.Get(p.baseURL + "/api/v1/proxy/version")
		if err == nil {
			resp.Body.Close()
			if resp.StatusCode == http.StatusOK {
				return
			}
		}
		time.Sleep(300 * time.Millisecond)
	}
	t.Fatalf("proxy subprocess HTTP server did not become ready in time.\nstdout:\n%s\nstderr:\n%s", p.stdout.String(), p.stderr.String())
}

func waitDeviceConnected(t *testing.T, p *proxyProcess) {
	t.Helper()
	deadline := time.Now().Add(15 * time.Second)
	for time.Now().Before(deadline) {
		resp, err := p.client.Get(p.baseURL + "/api/v1/firmware/version")
		if err == nil {
			var v struct {
				Version string `json:"version"`
			}
			_ = json.NewDecoder(resp.Body).Decode(&v)
			resp.Body.Close()
			if v.Version != "" {
				return
			}
		}
		time.Sleep(300 * time.Millisecond)
	}
	t.Fatalf("proxy subprocess did not connect to the device on %s in time.\nstdout:\n%s\nstderr:\n%s", hwtestPort(), p.stdout.String(), p.stderr.String())
}

func (p *proxyProcess) get(t *testing.T, path string) (*http.Response, []byte) {
	t.Helper()
	resp, err := p.client.Get(p.baseURL + path)
	require.NoError(t, err, "GET %s failed", path)
	body, err := io.ReadAll(resp.Body)
	resp.Body.Close()
	require.NoError(t, err)
	return resp, body
}

func (p *proxyProcess) post(t *testing.T, path string, payload string) (*http.Response, []byte) {
	t.Helper()
	resp, err := p.client.Post(p.baseURL+path, "application/json", strings.NewReader(payload))
	require.NoError(t, err, "POST %s failed", path)
	body, err := io.ReadAll(resp.Body)
	resp.Body.Close()
	require.NoError(t, err)
	return resp, body
}
