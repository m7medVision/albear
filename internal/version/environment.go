package version

import (
	"fmt"
	"os"
)

// Environment separates the albear a developer is building from the one that
// holds their real secrets. Each environment has its own data, config and
// runtime folders, so a dev build can never open the prod vault.
type Environment string

const (
	// Prod is every release build: a stamped release version, or one
	// recovered from build info for `go install …@vX.Y.Z`.
	Prod Environment = "prod"
	// Dev is every local build, which keeps the placeholder version "dev".
	Dev Environment = "dev"
)

// EnvVar overrides the environment derived from the version, e.g. to point a
// release build at the dev vault. Only "dev" and "prod" are accepted.
const EnvVar = "ALBEAR_ENV"

// EnvironmentOf derives the environment from a version string. Only a real
// release version is prod; the "dev" placeholder, and anything else that is
// not a version, is dev, so an unrecognised build fails safe away from the
// prod vault.
func EnvironmentOf(v string) Environment {
	if IsValid(v) {
		return Prod
	}
	return Dev
}

// ParseEnvironment accepts exactly "dev" or "prod".
func ParseEnvironment(s string) (Environment, error) {
	switch e := Environment(s); e {
	case Prod, Dev:
		return e, nil
	}
	return "", fmt.Errorf("version: invalid %s %q (want %q or %q)", EnvVar, s, Dev, Prod)
}

// CurrentEnvironment is the environment this process runs in: ALBEAR_ENV when
// set, otherwise the one derived from Version. An invalid ALBEAR_ENV is an
// error rather than a silent fallback, because guessing wrong means opening
// the other environment's vault.
func CurrentEnvironment() (Environment, error) {
	if s, ok := os.LookupEnv(EnvVar); ok && s != "" {
		return ParseEnvironment(s)
	}
	return EnvironmentOf(Version), nil
}
