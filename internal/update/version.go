package update

import (
	"fmt"
	"strconv"
	"strings"
)

// SemVer represents a parsed semantic version.
type SemVer struct {
	Major int
	Minor int
	Patch int
	Raw   string
}

// ParseSemVer parses a version string like "v1.2.3" or "1.2.3".
// Returns an error for non-parseable strings (like "dev").
func ParseSemVer(v string) (SemVer, error) {
	raw := v
	v = strings.TrimPrefix(v, "v")

	// Strip build metadata and pre-release suffix for comparison.
	if idx := strings.IndexAny(v, "-+"); idx != -1 {
		v = v[:idx]
	}

	parts := strings.Split(v, ".")
	if len(parts) < 2 || len(parts) > 3 {
		return SemVer{}, fmt.Errorf("invalid semver: %q", raw)
	}

	major, err := strconv.Atoi(parts[0])
	if err != nil {
		return SemVer{}, fmt.Errorf("invalid major version in %q: %w", raw, err)
	}
	minor, err := strconv.Atoi(parts[1])
	if err != nil {
		return SemVer{}, fmt.Errorf("invalid minor version in %q: %w", raw, err)
	}
	patch := 0
	if len(parts) == 3 {
		patch, err = strconv.Atoi(parts[2])
		if err != nil {
			return SemVer{}, fmt.Errorf("invalid patch version in %q: %w", raw, err)
		}
	}

	return SemVer{Major: major, Minor: minor, Patch: patch, Raw: raw}, nil
}

// Compare returns -1 if s < other, 0 if s == other, +1 if s > other.
func (s SemVer) Compare(other SemVer) int {
	if s.Major != other.Major {
		return cmp(s.Major, other.Major)
	}
	if s.Minor != other.Minor {
		return cmp(s.Minor, other.Minor)
	}
	if s.Patch != other.Patch {
		return cmp(s.Patch, other.Patch)
	}
	return 0
}

func cmp(a, b int) int {
	if a < b {
		return -1
	}
	return 1
}

// IsNewer returns true if current is semantically older than candidate.
// If either version is unparseable (e.g. "dev"), returns false.
func IsNewer(current, candidate string) bool {
	cur, err := ParseSemVer(current)
	if err != nil {
		return false
	}
	cand, err := ParseSemVer(candidate)
	if err != nil {
		return false
	}
	return cur.Compare(cand) < 0
}
