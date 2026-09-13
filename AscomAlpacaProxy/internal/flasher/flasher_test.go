package flasher

import (
	"testing"
	"testing/fstest"
)

func TestIsVersionOlder(t *testing.T) {
	cases := []struct {
		a, b string
		want bool
	}{
		{"0.9.14", "0.9.15", true},
		{"0.9.15", "0.9.15", false},
		{"0.9.16", "0.9.15", false},
		{"0.9.9", "0.9.15", true},   // segment-wise, not lexical, comparison
		{"1.0.0", "0.9.15", false},
		{"0.9.40-daily.abc123", "0.9.15", false}, // pre-release suffix must be stripped before comparing
		{"0.9", "0.9.15", true},                  // missing trailing segment reads as 0
	}
	for _, c := range cases {
		if got := isVersionOlder(c.a, c.b); got != c.want {
			t.Errorf("isVersionOlder(%q, %q) = %v, want %v", c.a, c.b, got, c.want)
		}
	}
}

// withBundledVersion sets bundledFirmwareVersion for the duration of one test, exactly the way
// Init would have via a real embedded release_version.json, without needing a real fs.FS.
func withBundledVersion(t *testing.T, v string) {
	t.Helper()
	old := bundledFirmwareVersion
	bundledFirmwareVersion = v
	t.Cleanup(func() { bundledFirmwareVersion = old })
}

func TestInit_ParsesReleaseVersionJSON(t *testing.T) {
	Init(fstest.MapFS{}, []byte(`{"proxyVersion":"0.9.40","firmwareVersion":"0.9.40"}`))
	t.Cleanup(func() { bundledFirmwareVersion = "unknown" })

	if bundledFirmwareVersion != "0.9.40" {
		t.Errorf("bundledFirmwareVersion = %q, want \"0.9.40\"", bundledFirmwareVersion)
	}
}

func TestInit_MalformedReleaseVersionJSONLeavesUnknown(t *testing.T) {
	// Regression guard for the previous design's failure mode (a missing/unreadable version
	// source silently reported as "unknown" is fine and expected here - unlike the old
	// firmware/version.json-under-flasherFS approach, a malformed *embedded* file indicates a
	// real build problem, but GetInfo must still degrade gracefully rather than erroring out).
	Init(fstest.MapFS{}, []byte(`not json`))
	t.Cleanup(func() { bundledFirmwareVersion = "unknown" })

	if bundledFirmwareVersion != "unknown" {
		t.Errorf("bundledFirmwareVersion = %q, want \"unknown\" after malformed input", bundledFirmwareVersion)
	}
}

func TestGetInfo_NotConnectedNoBundledVersion(t *testing.T) {
	// Neither Init() nor withBundledVersion() called - both InstalledVersion (nothing connects to
	// a device in this test) and BundledVersion (package-level default) read "unknown". GetInfo
	// must report that plainly rather than failing outright.
	info := GetInfo()
	if info.InstalledVersion != "unknown" {
		t.Errorf("InstalledVersion = %q, want \"unknown\"", info.InstalledVersion)
	}
	if info.BundledVersion != "unknown" {
		t.Errorf("BundledVersion = %q, want \"unknown\" (bundledFirmwareVersion never set in this test)", info.BundledVersion)
	}
}

func TestGetInfo_UnknownInstalled(t *testing.T) {
	withBundledVersion(t, "0.9.40")

	info := GetInfo()
	if info.InstalledVersion != "unknown" {
		t.Errorf("InstalledVersion = %q, want \"unknown\" (nothing in this test connects to a device)", info.InstalledVersion)
	}
	if info.BundledVersion != "0.9.40" {
		t.Errorf("BundledVersion = %q, want \"0.9.40\"", info.BundledVersion)
	}
	if info.UpToDate {
		t.Error("UpToDate = true, want false when installed version is unknown")
	}
	if info.ForceErase {
		t.Error("ForceErase = true, want false when installed version is unknown (nothing to force yet)")
	}
}

func TestGetInfo_MissingBundledVersionDoesNotHideInstalled(t *testing.T) {
	// Regression test for the bug reported from real usage: a build where the bundled version
	// couldn't be determined must still report whatever InstalledVersion is currently known, not
	// fail the whole response (the original bug behind this: GetInfo used to read the bundled
	// version from a file - firmware/version.json under flasherFS - that only the full
	// build_exe.bat/build_linux.sh wrote, so a plain `go build`/`vite build` made the *entire*
	// endpoint fail and hid a perfectly good InstalledVersion behind a client-side "Not
	// connected" the user had no way to tell apart from an actual missing device).
	withBundledVersion(t, "unknown")

	info := GetInfo()
	if info.BundledVersion != "unknown" {
		t.Errorf("BundledVersion = %q, want \"unknown\"", info.BundledVersion)
	}
	// InstalledVersion is "unknown" here too (nothing in this test connects to a device), but the
	// point is GetInfo returned a normal Info at all instead of an error/zero-value - the real
	// regression this guards against only manifests once a device IS connected, which unit tests
	// in this package can't simulate without real hardware (see internal/serial, which owns the
	// connection state GetInfo reads via GetFirmwareVersion()).
}
