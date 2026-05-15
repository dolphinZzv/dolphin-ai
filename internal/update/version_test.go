package update

import (
	"testing"
)

func TestParseSemVer(t *testing.T) {
	tests := []struct {
		input   string
		want    SemVer
		wantErr bool
	}{
		{"v1.0.0", SemVer{1, 0, 0, "v1.0.0"}, false},
		{"v2.1.3", SemVer{2, 1, 3, "v2.1.3"}, false},
		{"1.0.0", SemVer{1, 0, 0, "1.0.0"}, false},
		{"v10.20.30", SemVer{10, 20, 30, "v10.20.30"}, false},
		{"v1.2", SemVer{1, 2, 0, "v1.2"}, false},
		{"v1.2.3-beta", SemVer{1, 2, 3, "v1.2.3-beta"}, false},
		{"v1.2.3+build123", SemVer{1, 2, 3, "v1.2.3+build123"}, false},
		{"dev", SemVer{}, true},
		{"", SemVer{}, true},
		{"abc", SemVer{}, true},
		{"v1", SemVer{}, true},
		{"v1.0.0.0", SemVer{}, true},
	}

	for _, tt := range tests {
		t.Run(tt.input, func(t *testing.T) {
			got, err := ParseSemVer(tt.input)
			if tt.wantErr {
				if err == nil {
					t.Errorf("ParseSemVer(%q) expected error, got %+v", tt.input, got)
				}
				return
			}
			if err != nil {
				t.Errorf("ParseSemVer(%q) unexpected error: %v", tt.input, err)
				return
			}
			if got.Major != tt.want.Major || got.Minor != tt.want.Minor || got.Patch != tt.want.Patch {
				t.Errorf("ParseSemVer(%q) = {%d,%d,%d}, want {%d,%d,%d}",
					tt.input, got.Major, got.Minor, got.Patch,
					tt.want.Major, tt.want.Minor, tt.want.Patch)
			}
		})
	}
}

func TestSemVerCompare(t *testing.T) {
	tests := []struct {
		a, b string
		want int
	}{
		{"v1.0.0", "v1.0.0", 0},
		{"v1.0.0", "v1.0.1", -1},
		{"v1.0.1", "v1.0.0", 1},
		{"v1.0.0", "v1.1.0", -1},
		{"v1.9.0", "v1.10.0", -1},
		{"v1.0.0", "v2.0.0", -1},
		{"v2.0.0", "v1.9.9", 1},
		{"v10.0.0", "v2.0.0", 1},
	}

	for _, tt := range tests {
		t.Run(tt.a+"_vs_"+tt.b, func(t *testing.T) {
			a, _ := ParseSemVer(tt.a)
			b, _ := ParseSemVer(tt.b)
			got := a.Compare(b)
			if got != tt.want {
				t.Errorf("%s.Compare(%s) = %d, want %d", tt.a, tt.b, got, tt.want)
			}
		})
	}
}

func TestIsNewer(t *testing.T) {
	tests := []struct {
		current, candidate string
		want               bool
	}{
		{"v1.0.0", "v1.0.1", true},
		{"v1.0.0", "v2.0.0", true},
		{"v1.9.0", "v1.10.0", true},
		{"v2.0.0", "v1.9.9", false},
		{"v1.0.0", "v1.0.0", false},
		{"dev", "v1.0.0", false},
		{"v1.0.0", "dev", false},
		{"dev", "dev", false},
	}

	for _, tt := range tests {
		t.Run(tt.current+"_vs_"+tt.candidate, func(t *testing.T) {
			got := IsNewer(tt.current, tt.candidate)
			if got != tt.want {
				t.Errorf("IsNewer(%q, %q) = %v, want %v", tt.current, tt.candidate, got, tt.want)
			}
		})
	}
}
