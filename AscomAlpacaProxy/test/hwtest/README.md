# Hardware-in-the-Loop Testsuite

Go test suite that exercises the real SV241 firmware and the real Proxy against a physically
connected box. Gated behind the `hwtest` build tag, so it never runs as part of a normal
`go build`/`go test ./...` and never needs hardware to be present for those.

## Prerequisites

- A box connected over serial (default `COM5`; override with `HWTEST_PORT`).
- **No other process may be holding the port** - close the real Proxy app, any serial monitor,
  and the web flasher page before running this suite.
- The Proxy E2E tests build a fresh proxy binary from source, which needs the frontend already
  built (`AscomAlpacaProxy/frontend-vue/dist` present - run `npm run build` in `frontend-vue/`
  first if it isn't).
- `TestConfig_PersistenceAcrossReset` resets the device via `esptool`. It auto-detects
  PlatformIO's bundled Python/esptool (`%USERPROFILE%\.platformio\...`); override with
  `HWTEST_PYTHON` / `HWTEST_ESPTOOL_PY` if yours lives elsewhere. If neither can be found, that
  one test is skipped (not failed) rather than blocking the rest of the suite.

## ⚠️ This suite switches real outputs on and off

Running it will toggle DC/USB switches, the adjustable converter, and both dew heaters
repeatedly. Disconnect anything you don't want power-cycled (or don't want to see flicker)
before running it.

## Running

```sh
cd AscomAlpacaProxy
go test -tags hwtest ./test/hwtest/... -v -count=1
```

The suite captures a full config snapshot before the first test runs and restores it exactly
after the last one finishes (even on failure) - see `main_test.go`'s `TestMain`. Individual tests
also try to leave their own corner of state clean, but the snapshot restore is the actual safety
net.

**Always pass `-count=1`.** This suite has real side effects (it talks to physical hardware, and
the Proxy E2E tests rebuild the proxy binary from whatever's currently in `frontend-vue/dist`) that
Go's test result cache knows nothing about. Without `-count=1`, an unchanged `hwtest` package will
make `go test` silently replay a *stale cached PASS* instead of actually touching the box or
rebuilding the proxy - easy to miss, since the output looks identical except for `(cached)` next
to the final `ok` line.

Override the port:

```sh
HWTEST_PORT=COM7 go test -tags hwtest ./test/hwtest/... -v -count=1
```

## Burn-in test

`TestBurnIn` is a long-running heap-stability stress test (repeated switch/heater/config
operations while sampling `hf`/`hmf`/`hma`/`hs` from `{"get":"sensors"}`) - formalizes this
project's manual heap-fragmentation investigation. It does **not** run by default:

```sh
# Quick 5-minute smoke test
BURNIN=1 go test -tags hwtest -run TestBurnIn ./test/hwtest/... -v -count=1

# Longer run before a firmware release (recommended: 30-60+ minutes)
BURNIN=1 BURNIN_DURATION=1h go test -tags hwtest -run TestBurnIn ./test/hwtest/... -v -count=1 -timeout 2h
```

(`-count=1` matters here too - `BURNIN_DURATION` is read via `os.Getenv` at runtime, which Go's
test cache doesn't track, so a second run with a different duration could otherwise be silently
skipped and replayed from cache.)

Note the `-timeout` flag: `go test`'s default overall timeout (10 minutes) will otherwise kill a
long burn-in run before it finishes.

## Layout

| File | Covers |
|---|---|
| `main_test.go` | `TestMain`: config snapshot/restore bracket around the whole run |
| `serial_helper_test.go` | Minimal JSON-over-serial protocol client (independent of `internal/serial`) |
| `firmware_helpers_test.go` | Shared domain helpers (get status/config, set switch/config) |
| `firmware_switches_test.go` | DC/USB/ADJ switches, `all`, staggering, Disabled interaction |
| `firmware_heaters_test.go` | Both dew heaters, all modes, `xd` clamp, Sync follower |
| `firmware_config_test.go` | Persistence across a real reset, `psd` clamp boundaries, unknown-key robustness |
| `firmware_burnin_test.go` | Opt-in long-running heap stability stress test |
| `proxy_e2e_helper_test.go` | Spawns/manages the real proxy binary as an isolated subprocess |
| `proxy_e2e_test.go` | REST + Alpaca Switch/ObservingConditions endpoints against the running proxy |
