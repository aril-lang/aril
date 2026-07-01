package main

import "testing"

func TestParseGoVersion(t *testing.T) {
	cases := []struct {
		raw          string
		norm         string
		major, minor int
		ok           bool
	}{
		{"go version go1.22.3 linux/amd64", "go1.22", 1, 22, true},
		{"go version go1.21 darwin/arm64", "go1.21", 1, 21, true},
		{"go version go1.23.0 windows/amd64", "go1.23", 1, 23, true},
		{"go version devel go1.24-abcdef X", "go1.24", 1, 24, true},
		{"not go output at all", "", 0, 0, false},
		{"", "", 0, 0, false},
	}
	for _, c := range cases {
		norm, maj, min, ok := parseGoVersion(c.raw)
		if ok != c.ok || norm != c.norm || maj != c.major || min != c.minor {
			t.Errorf("parseGoVersion(%q) = (%q,%d,%d,%v); want (%q,%d,%d,%v)",
				c.raw, norm, maj, min, ok, c.norm, c.major, c.minor, c.ok)
		}
	}
}

func TestSupportedGo(t *testing.T) {
	// Minimum is go1.22 (minGoMajor/minGoMinor).
	supported := [][2]int{{1, 22}, {1, 23}, {2, 0}}
	tooOld := [][2]int{{1, 21}, {1, 20}, {1, 0}}
	for _, v := range supported {
		if !supportedGo(v[0], v[1]) {
			t.Errorf("supportedGo(%d,%d) = false; want true (min %d.%d)", v[0], v[1], minGoMajor, minGoMinor)
		}
	}
	for _, v := range tooOld {
		if supportedGo(v[0], v[1]) {
			t.Errorf("supportedGo(%d,%d) = true; want false (min %d.%d)", v[0], v[1], minGoMajor, minGoMinor)
		}
	}
}
