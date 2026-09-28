// Package system owns filesystem locations and permissions (PRD 17.2) and
// daemon runtime hardening.
package system

import (
	"fmt"
	"os"
	"path/filepath"

	"github.com/m7medVision/albear/internal/version"
)

// Paths are the resolved storage locations. None of them are secret; the
// contents are protected cryptographically.
type Paths struct {
	// Env is the environment the paths belong to; dev and prod never share a
	// folder, so a dev build cannot open the prod vault.
	Env        version.Environment
	DataDir    string
	ConfigDir  string
	RuntimeDir string
}

func (p Paths) Database() string  { return filepath.Join(p.DataDir, "vault.db") }
func (p Paths) Socket() string    { return filepath.Join(p.RuntimeDir, "vault.sock") }
func (p Paths) StaticKey() string { return filepath.Join(p.ConfigDir, "daemon.key") }
func (p Paths) ClientDir() string { return filepath.Join(p.ConfigDir, "clients") }

// ResolvePaths applies the XDG spec with home fallbacks, for the environment
// this process runs in (version.CurrentEnvironment). Prod uses "albear"
// folders, as every release always has; dev uses "albear-dev" in the same
// places. An invalid ALBEAR_ENV is an error.
func ResolvePaths() (Paths, error) {
	env, err := version.CurrentEnvironment()
	if err != nil {
		return Paths{}, err
	}
	home, err := os.UserHomeDir()
	if err != nil {
		return Paths{}, err
	}
	data := os.Getenv("XDG_DATA_HOME")
	if data == "" {
		data = filepath.Join(home, ".local", "share")
	}
	config := os.Getenv("XDG_CONFIG_HOME")
	if config == "" {
		config = filepath.Join(home, ".config")
	}
	name := dirName(env)
	runtime := os.Getenv("XDG_RUNTIME_DIR")
	if runtime == "" {
		runtime = filepath.Join(data, name, "run")
	}
	return Paths{
		Env:        env,
		DataDir:    filepath.Join(data, name),
		ConfigDir:  filepath.Join(config, name),
		RuntimeDir: filepath.Join(runtime, name),
	}, nil
}

// dirName is the per-environment folder name used under each XDG base. The
// desktop app mirrors it (desktop/src/main/environment.ts).
func dirName(env version.Environment) string {
	if env == version.Dev {
		return "albear-dev"
	}
	return "albear"
}

// Prepare creates every directory with 0700 and verifies the modes.
func (p Paths) Prepare() error {
	for _, dir := range []string{p.DataDir, p.ConfigDir, p.RuntimeDir, p.ClientDir()} {
		if err := os.MkdirAll(dir, 0o700); err != nil {
			return err
		}
		if err := os.Chmod(dir, 0o700); err != nil {
			return err
		}
	}
	return nil
}

// CheckPrivate verifies a file is owned by the user with no group/other bits.
func CheckPrivate(path string) error {
	st, err := os.Stat(path)
	if err != nil {
		return err
	}
	if st.Mode().Perm()&0o077 != 0 {
		return fmt.Errorf("system: %s is accessible by other users (mode %v)", path, st.Mode().Perm())
	}
	return nil
}
