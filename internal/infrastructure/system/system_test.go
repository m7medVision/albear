package system

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/m7medVision/albear/internal/version"
)

// setVersion stamps version.Version for one test, as release ldflags would.
func setVersion(t *testing.T, v string) {
	t.Helper()
	old := version.Version
	version.Version = v
	t.Cleanup(func() { version.Version = old })
}

func TestResolvePathsXDG(t *testing.T) {
	cases := []struct {
		name     string
		version  string
		env      string
		wantEnv  version.Environment
		database string
		socket   string
		key      string
	}{
		{
			name: "release build is prod", version: "v1.2.3", wantEnv: version.Prod,
			database: "/tmp/x/data/albear/vault.db",
			socket:   "/tmp/x/run/albear/vault.sock",
			key:      "/tmp/x/config/albear/daemon.key",
		},
		{
			name: "local build is dev", version: "dev", wantEnv: version.Dev,
			database: "/tmp/x/data/albear-dev/vault.db",
			socket:   "/tmp/x/run/albear-dev/vault.sock",
			key:      "/tmp/x/config/albear-dev/daemon.key",
		},
		{
			name: "override release to dev", version: "v1.2.3", env: "dev", wantEnv: version.Dev,
			database: "/tmp/x/data/albear-dev/vault.db",
			socket:   "/tmp/x/run/albear-dev/vault.sock",
			key:      "/tmp/x/config/albear-dev/daemon.key",
		},
		{
			name: "override local build to prod", version: "dev", env: "prod", wantEnv: version.Prod,
			database: "/tmp/x/data/albear/vault.db",
			socket:   "/tmp/x/run/albear/vault.sock",
			key:      "/tmp/x/config/albear/daemon.key",
		},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			setVersion(t, c.version)
			t.Setenv(version.EnvVar, c.env)
			t.Setenv("XDG_DATA_HOME", "/tmp/x/data")
			t.Setenv("XDG_CONFIG_HOME", "/tmp/x/config")
			t.Setenv("XDG_RUNTIME_DIR", "/tmp/x/run")
			p, err := ResolvePaths()
			if err != nil {
				t.Fatal(err)
			}
			if p.Env != c.wantEnv {
				t.Errorf("Env = %q, want %q", p.Env, c.wantEnv)
			}
			if p.Database() != c.database {
				t.Errorf("Database() = %q, want %q", p.Database(), c.database)
			}
			if p.Socket() != c.socket {
				t.Errorf("Socket() = %q, want %q", p.Socket(), c.socket)
			}
			if p.StaticKey() != c.key {
				t.Errorf("StaticKey() = %q, want %q", p.StaticKey(), c.key)
			}
		})
	}
}

func TestResolvePathsFallback(t *testing.T) {
	home, err := os.UserHomeDir()
	if err != nil {
		t.Fatal(err)
	}
	share := filepath.Join(home, ".local", "share")
	cases := []struct {
		version, env, name string
	}{
		{version: "v1.2.3", name: "albear"},
		{version: "dev", name: "albear-dev"},
		{version: "v1.2.3", env: "dev", name: "albear-dev"},
		{version: "dev", env: "prod", name: "albear"},
	}
	for _, c := range cases {
		t.Run(c.version+"/"+c.env, func(t *testing.T) {
			setVersion(t, c.version)
			t.Setenv(version.EnvVar, c.env)
			t.Setenv("XDG_DATA_HOME", "")
			t.Setenv("XDG_CONFIG_HOME", "")
			t.Setenv("XDG_RUNTIME_DIR", "")
			p, err := ResolvePaths()
			if err != nil {
				t.Fatal(err)
			}
			if want := filepath.Join(share, c.name); p.DataDir != want {
				t.Errorf("DataDir = %q, want %q", p.DataDir, want)
			}
			if want := filepath.Join(home, ".config", c.name); p.ConfigDir != want {
				t.Errorf("ConfigDir = %q, want %q", p.ConfigDir, want)
			}
			// Without XDG_RUNTIME_DIR the socket lives under the environment's
			// own data folder, so dev and prod still never share one.
			if want := filepath.Join(share, c.name, "run", c.name, "vault.sock"); p.Socket() != want {
				t.Errorf("Socket() = %q, want %q", p.Socket(), want)
			}
		})
	}
}

// TestResolvePathsInvalidEnv: a typo in ALBEAR_ENV must fail loudly rather
// than fall back to a guess that could open the other environment's vault.
func TestResolvePathsInvalidEnv(t *testing.T) {
	for _, v := range []string{"v1.2.3", "dev"} {
		for _, env := range []string{"production", "DEV", "staging", " dev"} {
			setVersion(t, v)
			t.Setenv(version.EnvVar, env)
			if p, err := ResolvePaths(); err == nil {
				t.Errorf("version %q, %s=%q: resolved %+v, want error", v, version.EnvVar, env, p)
			}
		}
	}
}

func TestPrepareCreatesPrivateDirs(t *testing.T) {
	base := t.TempDir()
	p := Paths{
		DataDir:    filepath.Join(base, "data"),
		ConfigDir:  filepath.Join(base, "config"),
		RuntimeDir: filepath.Join(base, "run"),
	}
	if err := p.Prepare(); err != nil {
		t.Fatal(err)
	}
	for _, dir := range []string{p.DataDir, p.ConfigDir, p.RuntimeDir, p.ClientDir()} {
		st, err := os.Stat(dir)
		if err != nil {
			t.Fatal(err)
		}
		if st.Mode().Perm() != 0o700 {
			t.Fatalf("%s mode %v", dir, st.Mode().Perm())
		}
	}
}

func TestCheckPrivate(t *testing.T) {
	f := filepath.Join(t.TempDir(), "secret")
	os.WriteFile(f, []byte("x"), 0o600)
	if err := CheckPrivate(f); err != nil {
		t.Fatal(err)
	}
	os.Chmod(f, 0o644)
	if err := CheckPrivate(f); err == nil {
		t.Fatal("world-readable file passed")
	}
	if err := CheckPrivate(filepath.Join(t.TempDir(), "missing")); err == nil {
		t.Fatal("missing file passed")
	}
}

func TestStaticKeyPersistence(t *testing.T) {
	path := filepath.Join(t.TempDir(), "daemon.key")
	k1, err := LoadOrCreateStaticKey(path)
	if err != nil || len(k1.Public) != 32 {
		t.Fatal(err)
	}
	st, _ := os.Stat(path)
	if st.Mode().Perm() != 0o600 {
		t.Fatalf("key file mode %v", st.Mode().Perm())
	}
	// Second load returns the same key.
	k2, err := LoadOrCreateStaticKey(path)
	if err != nil {
		t.Fatal(err)
	}
	if string(k1.Public) != string(k2.Public) || string(k1.Private) != string(k2.Private) {
		t.Fatal("static key not stable across loads")
	}
	// Corrupt file fails loudly rather than silently regenerating.
	os.WriteFile(path, []byte("garbage"), 0o600)
	if _, err := LoadOrCreateStaticKey(path); err == nil {
		t.Fatal("corrupt key file accepted")
	}
}

// TestStaticKeyRefusesLoosePermissions: the private half is the daemon's
// transport identity. A readable copy means another user may already have
// impersonated the daemon, so loading fails closed rather than carrying on.
func TestStaticKeyRefusesLoosePermissions(t *testing.T) {
	for _, mode := range []os.FileMode{0o644, 0o640, 0o604, 0o666, 0o660} {
		path := filepath.Join(t.TempDir(), "daemon.key")
		if _, err := LoadOrCreateStaticKey(path); err != nil {
			t.Fatal(err)
		}
		if err := os.Chmod(path, mode); err != nil {
			t.Fatal(err)
		}
		if _, err := LoadOrCreateStaticKey(path); err == nil {
			t.Fatalf("mode %v accepted", mode)
		}
	}
	// 0600 and stricter still load.
	for _, mode := range []os.FileMode{0o600, 0o400} {
		path := filepath.Join(t.TempDir(), "daemon.key")
		if _, err := LoadOrCreateStaticKey(path); err != nil {
			t.Fatal(err)
		}
		if err := os.Chmod(path, mode); err != nil {
			t.Fatal(err)
		}
		if _, err := LoadOrCreateStaticKey(path); err != nil {
			t.Fatalf("mode %v rejected: %v", mode, err)
		}
	}
}

// TestStaticKeyCreateRefusesExistingTarget: O_EXCL means creation neither
// follows a symlink planted at the path nor clobbers a key already there.
func TestStaticKeyCreateRefusesExistingTarget(t *testing.T) {
	dir := t.TempDir()

	// A symlink pointing somewhere the daemon must not write. Creation must
	// not follow it: the file is unparseable as a key, so the load path errors
	// and the create path must refuse rather than overwrite the target.
	target := filepath.Join(dir, "victim")
	if err := os.WriteFile(target, []byte("do not clobber"), 0o600); err != nil {
		t.Fatal(err)
	}
	link := filepath.Join(dir, "link.key")
	if err := os.Symlink(target, link); err != nil {
		t.Fatal(err)
	}
	if _, err := LoadOrCreateStaticKey(link); err == nil {
		t.Fatal("planted symlink accepted")
	}
	if got, _ := os.ReadFile(target); string(got) != "do not clobber" {
		t.Fatal("creation followed the symlink and overwrote the target")
	}

	// A dangling symlink: ReadFile fails with IsNotExist, so creation runs and
	// O_EXCL must stop it from writing through the link.
	dangling := filepath.Join(dir, "dangling.key")
	if err := os.Symlink(filepath.Join(dir, "nowhere"), dangling); err != nil {
		t.Fatal(err)
	}
	if _, err := LoadOrCreateStaticKey(dangling); err == nil {
		t.Fatal("dangling symlink accepted")
	}
	if _, err := os.Stat(filepath.Join(dir, "nowhere")); err == nil {
		t.Fatal("creation wrote through a dangling symlink")
	}
}
