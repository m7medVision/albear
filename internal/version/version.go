// Package version holds the build-time version stamp and hand-rolled semver
// helpers (stdlib only; no external semver dependency).
package version

import (
	"regexp"
	"runtime/debug"
	"strconv"
	"strings"
)

// Version is stamped by release builds via
//
//	go build -ldflags "-X github.com/m7medVision/albear/internal/version.Version=v1.2.3"
//
// and stays "dev" for local builds.
var Version = "dev"

// pseudoVersionRE matches the pseudo-versions the go command synthesizes from
// VCS state (e.g. v0.0.0-20260716185608-d9fd70567ba9). Mirrors
// golang.org/x/mod/module.pseudoVersionRE; inlined to keep this package
// stdlib-only.
var pseudoVersionRE = regexp.MustCompile(`^v[0-9]+\.(0\.0-|[0-9]+\.[0-9]+-([^+]*\.)?0\.)[0-9]{14}-[A-Za-z0-9]+(\+[0-9A-Za-z-]+(\.[0-9A-Za-z-]+)*)?$`)

// go install cannot pass -ldflags, so binaries installed that way would report
// "dev" forever — which also silences update checks, since update.Checker
// treats "dev" as a build that must never phone home. Recover the version the
// module system recorded for `go install <pkg>@v1.2.3` instead.
//
// Only an exact release tag counts. Since Go 1.24 a plain `go build` stamps a
// VCS-derived pseudo-version rather than "(devel)", and a dirty tree adds
// "+dirty"; treating either as a release would turn on update checks for local
// development builds and compare a v0.0.0-<timestamp> pseudo-version against
// every release, reporting an update forever.
func init() {
	if Version != "dev" {
		return
	}
	if info, ok := debug.ReadBuildInfo(); ok {
		if v, ok := releaseVersion(info.Main.Version); ok {
			Version = v
		}
	}
}

// releaseVersion reports whether a runtime/debug main-module version came from
// an exact release tag, and is therefore safe to report and to check updates
// against.
func releaseVersion(v string) (string, bool) {
	if v == "" || v == "(devel)" || strings.Contains(v, "+dirty") {
		return "", false
	}
	if pseudoVersionRE.MatchString(v) {
		return "", false
	}
	if !IsValid(v) {
		return "", false
	}
	return v, true
}

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
