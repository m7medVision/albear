package system

import (
	"os"
	"path/filepath"
	"testing"
)

func TestResolvePathsXDG(t *testing.T) {
	t.Setenv("XDG_DATA_HOME", "/tmp/x/data")
	t.Setenv("XDG_CONFIG_HOME", "/tmp/x/config")
	t.Setenv("XDG_RUNTIME_DIR", "/tmp/x/run")
	p, err := ResolvePaths()
	if err != nil {
		t.Fatal(err)
	}
	if p.Database() != "/tmp/x/data/albear/vault.db" {
		t.Fatal(p.Database())
	}
	if p.Socket() != "/tmp/x/run/albear/vault.sock" {
		t.Fatal(p.Socket())
	}
	if p.StaticKey() != "/tmp/x/config/albear/daemon.key" {
		t.Fatal(p.StaticKey())
	}
}

func TestResolvePathsFallback(t *testing.T) {
	t.Setenv("XDG_DATA_HOME", "")
	t.Setenv("XDG_CONFIG_HOME", "")
	t.Setenv("XDG_RUNTIME_DIR", "")
	home, _ := os.UserHomeDir()
	p, err := ResolvePaths()
	if err != nil {
		t.Fatal(err)
	}
	if p.DataDir != filepath.Join(home, ".local", "share", "albear") {
		t.Fatal(p.DataDir)
	}
	if p.ConfigDir != filepath.Join(home, ".config", "albear") {
		t.Fatal(p.ConfigDir)
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
