// Package version holds the build-time version stamp and hand-rolled semver
// helpers (stdlib only; no external semver dependency).
package version

import (
	"strconv"
	"strings"
)

// Version is stamped by release builds via
//
//	go build -ldflags "-X albear/internal/version.Version=v1.2.3"
//
// and stays "dev" for local builds.
var Version = "dev"

// Normalize trims whitespace and a single leading "v"/"V" so tags and bare
// versions compare equal ("v1.2.3" == "1.2.3").
func Normalize(tag string) string {
	tag = strings.TrimSpace(tag)
	if len(tag) > 0 && (tag[0] == 'v' || tag[0] == 'V') {
		tag = tag[1:]
	}
	return tag
}

// IsValid reports whether tag parses as a semver version, with or without a
// leading "v".
func IsValid(tag string) bool {
	_, ok := parse(tag)
	return ok
}

// IsNewer reports whether candidate is a valid semver tag strictly newer than
// current. It is false whenever either side does not parse (e.g. "dev"), so
// callers never announce updates based on garbage tags.
func IsNewer(candidate, current string) bool {
	return IsValid(candidate) && IsValid(current) && Compare(candidate, current) > 0
}

// Compare orders two semver tags per semver.org precedence: -1 when a < b,
// 0 when equal, +1 when a > b. Build metadata is ignored; an invalid tag
// compares lower than any valid one.
func Compare(a, b string) int {
	pa, aOK := parse(a)
	pb, bOK := parse(b)
	switch {
	case !aOK && !bOK:
		return 0
	case !aOK:
		return -1
	case !bOK:
		return 1
	}
	for i := range pa.nums {
		if c := compareInt(pa.nums[i], pb.nums[i]); c != 0 {
			return c
		}
	}
	return comparePre(pa.pre, pb.pre)
}

type parsed struct {
	nums [3]int
	pre  []string
}

// parse accepts MAJOR[.MINOR[.PATCH]][-PRERELEASE][+BUILD].
func parse(tag string) (parsed, bool) {
	s := Normalize(tag)
	if i := strings.IndexByte(s, '+'); i >= 0 { // build metadata never orders
		s = s[:i]
	}
	var p parsed
	if i := strings.IndexByte(s, '-'); i >= 0 {
		s, p.pre = s[:i], strings.Split(s[i+1:], ".")
	}
	fields := strings.Split(s, ".")
	if len(fields) > 3 {
		return parsed{}, false
	}
	for i, f := range fields {
		n, err := strconv.Atoi(f)
		if err != nil || n < 0 {
			return parsed{}, false
		}
		p.nums[i] = n
	}
	return p, true
}

// comparePre orders pre-release identifier lists; a release (no identifiers)
// outranks any pre-release of the same core version.
func comparePre(a, b []string) int {
	switch {
	case len(a) == 0 && len(b) == 0:
		return 0
	case len(a) == 0:
		return 1
	case len(b) == 0:
		return -1
	}
	for i := 0; i < len(a) && i < len(b); i++ {
		if c := compareIdent(a[i], b[i]); c != 0 {
			return c
		}
	}
	return compareInt(len(a), len(b))
}

// compareIdent applies semver identifier precedence: numeric identifiers
// compare numerically and rank below alphanumeric ones.
func compareIdent(a, b string) int {
	an, aErr := strconv.Atoi(a)
	bn, bErr := strconv.Atoi(b)
	switch {
	case aErr == nil && bErr == nil:
		return compareInt(an, bn)
	case aErr == nil:
		return -1
	case bErr == nil:
		return 1
	}
	return strings.Compare(a, b)
}

func compareInt(a, b int) int {
	switch {
	case a < b:
		return -1
	case a > b:
		return 1
	}
	return 0
}
