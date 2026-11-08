package domain

import (
	"fmt"
	"strconv"
	"strings"
)

// Version is a release version, MAJOR.MINOR.PATCH (the VERSION file; M27).
type Version struct{ Major, Minor, Patch int }

// ParseVersion reads "1.13.0" (a leading "v" is allowed). Anything else — a development
// build ("dev"), a pre-release suffix — is ErrValidation.
func ParseVersion(s string) (Version, error) {
	parts := strings.Split(strings.TrimPrefix(strings.TrimSpace(s), "v"), ".")
	if len(parts) != 3 {
		return Version{}, fmt.Errorf("%w: version %q is not MAJOR.MINOR.PATCH", ErrValidation, s)
	}
	var n [3]int
	for i, p := range parts {
		v, err := strconv.Atoi(p)
		if err != nil || v < 0 || p != strconv.Itoa(v) {
			return Version{}, fmt.Errorf("%w: version %q is not MAJOR.MINOR.PATCH", ErrValidation, s)
		}
		n[i] = v
	}
	return Version{Major: n[0], Minor: n[1], Patch: n[2]}, nil
}

// Less reports whether v is older than o.
func (v Version) Less(o Version) bool {
	if v.Major != o.Major {
		return v.Major < o.Major
	}
	if v.Minor != o.Minor {
		return v.Minor < o.Minor
	}
	return v.Patch < o.Patch
}

func (v Version) String() string { return fmt.Sprintf("%d.%d.%d", v.Major, v.Minor, v.Patch) }
