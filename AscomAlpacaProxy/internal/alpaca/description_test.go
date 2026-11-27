package alpaca

import "testing"

func TestDescriptionLocation(t *testing.T) {
	cases := []struct {
		name      string
		connected bool
		rigName   string
		want      string
	}{
		{"connected and named", true, "Test Rig", "Test Rig"},
		{"connected, name trimmed", true, "  Imaging Rig  ", "Imaging Rig"},
		{"connected but unnamed", true, "", genericLocation},
		{"connected, whitespace-only name", true, "   ", genericLocation},
		{"disconnected keeps stale name out", false, "Test Rig", genericLocation},
		{"disconnected and unnamed", false, "", genericLocation},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if got := descriptionLocation(tc.connected, tc.rigName); got != tc.want {
				t.Fatalf("descriptionLocation(%v, %q) = %q, want %q", tc.connected, tc.rigName, got, tc.want)
			}
		})
	}
}
