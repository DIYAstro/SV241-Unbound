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

func TestGetInfo_NotConnected(t *testing.T) {
	// No Init() call in this test - flasherFS is nil, and serial.GetFirmwareVersion() reads
	// this package's untouched default ("unknown", since nothing here ever calls
	// serial.StartManager()). GetInfo must report that plainly rather than erroring out just
	// because there's no bundled-version file to read yet.
	info, err := GetInfo()
	if err == nil {
		t.Fatalf("GetInfo() with flasherFS unset: expected an error (flasher not initialized), got info=%+v", info)
	}
}

func TestGetInfo_UnknownInstalled(t *testing.T) {
	flasherFS = fstest.MapFS{
		"firmware/version.json": &fstest.MapFile{Data: []byte(`{"version":"0.9.40"}`)},
	}
	defer func() { flasherFS = nil }()

	info, err := GetInfo()
	if err != nil {
		t.Fatalf("GetInfo() returned an error: %v", err)
	}
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
