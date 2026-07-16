package version

import "testing"

func TestNormalize(t *testing.T) {
	for in, want := range map[string]string{
		"v1.2.3":  "1.2.3",
		"V1.2.3":  "1.2.3",
		" v0.1.0": "0.1.0",
		"1.2.3":   "1.2.3",
		"":        "",
	} {
		if got := Normalize(in); got != want {
			t.Errorf("Normalize(%q) = %q, want %q", in, got, want)
		}
	}
}

func TestCompare(t *testing.T) {
	cases := []struct {
		a, b string
		want int
	}{
		{"v1.2.3", "1.2.3", 0},
		{"v1.2.3", "v1.2.4", -1},
		{"v1.3.0", "v1.2.9", 1},
		{"v2.0.0", "v1.99.99", 1},
		{"v1.2", "v1.2.0", 0},
		{"v1", "v1.0.0", 0},
		{"v0.10.0", "v0.9.0", 1},
		// Pre-release ranks below the release of the same core.
		{"v1.0.0-rc.1", "v1.0.0", -1},
		{"v1.0.0", "v1.0.0-rc.1", 1},
		{"v1.0.0-alpha", "v1.0.0-beta", -1},
		{"v1.0.0-alpha.1", "v1.0.0-alpha", 1},
		{"v1.0.0-2", "v1.0.0-11", -1},
		{"v1.0.0-1", "v1.0.0-alpha", -1},
		{"v1.0.0-rc.1", "v1.0.0-rc.1", 0},
		// Build metadata never orders.
		{"v1.2.3+build.5", "v1.2.3+build.9", 0},
		// Invalid tags compare lowest.
		{"dev", "v0.0.1", -1},
		{"v0.0.1", "garbage", 1},
		{"dev", "junk", 0},
	}
	for _, c := range cases {
		if got := Compare(c.a, c.b); got != c.want {
			t.Errorf("Compare(%q, %q) = %d, want %d", c.a, c.b, got, c.want)
		}
	}
}

func TestIsValid(t *testing.T) {
	for tag, want := range map[string]bool{
		"v1.2.3":       true,
		"1.2.3":        true,
		"v1.2":         true,
		"v1.0.0-rc.1":  true,
		"v1.2.3+sha.1": true,
		"dev":          false,
		"":             false,
		"v1.2.3.4":     false,
		"v1..2":        false,
		"v-1.2.3":      false,
	} {
		if got := IsValid(tag); got != want {
			t.Errorf("IsValid(%q) = %v, want %v", tag, got, want)
		}
	}
}

func TestIsNewer(t *testing.T) {
	cases := []struct {
		candidate, current string
		want               bool
	}{
		{"v0.2.0", "v0.1.0", true},
		{"v0.1.0", "v0.2.0", false},
		{"v0.1.0", "v0.1.0", false},
		{"v1.0.0", "v1.0.0-rc.1", true},
		// Anything involving an unparseable side is never "newer".
		{"v9.9.9", "dev", false},
		{"dev", "v0.1.0", false},
		{"", "v0.1.0", false},
	}
	for _, c := range cases {
		if got := IsNewer(c.candidate, c.current); got != c.want {
			t.Errorf("IsNewer(%q, %q) = %v, want %v", c.candidate, c.current, got, c.want)
		}
	}
}
