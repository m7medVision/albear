package install

import (
	"encoding/json"
	"errors"
	"io/fs"
	"os"
	"path/filepath"
	"reflect"
	"testing"
)

// browserConfigDirs is each supported browser's per-user config folder,
// relative to XDG_CONFIG_HOME.
var browserConfigDirs = map[string]string{
	"brave":    filepath.Join("BraveSoftware", "Brave-Browser"),
	"chrome":   "google-chrome",
	"chromium": "chromium",
	"helium":   "net.imput.helium",
}

// installFixture points XDG_CONFIG_HOME at a temp dir and returns it with
// Options naming a fake executable native host and extension dir, and an
// empty system root so nothing installed on the machine leaks in.
func installFixture(t *testing.T) (string, Options) {
	t.Helper()
	dir := t.TempDir()
	hostPath := filepath.Join(dir, "vault-native")
	if err := os.WriteFile(hostPath, []byte("#!/bin/sh\n"), 0o755); err != nil {
		t.Fatal(err)
	}
	extDir := filepath.Join(dir, "ext")
	if err := os.MkdirAll(extDir, 0o755); err != nil {
		t.Fatal(err)
	}
	config := filepath.Join(dir, "config")
	t.Setenv("XDG_CONFIG_HOME", config)
	root := filepath.Join(dir, "root")
	return config, Options{NativeHostPath: hostPath, ExtensionDir: extDir, SystemRoot: root}
}

func manifestFor(config, browser, host string) string {
	return filepath.Join(config, browserConfigDirs[browser], "NativeMessagingHosts", host+".json")
}

func exists(t *testing.T, path string) bool {
	t.Helper()
	_, err := os.Stat(path)
	if errors.Is(err, fs.ErrNotExist) {
		return false
	}
	if err != nil {
		t.Fatal(err)
	}
	return true
}

func TestRegistryListsChromiumFamily(t *testing.T) {
	want := []string{"brave", "chrome", "chromium", "helium"}
	if got := Names(); !reflect.DeepEqual(got, want) {
		t.Fatalf("Names() = %v, want %v", got, want)
	}
	for _, s := range All() {
		// Google Chrome on Linux has no per-user External Extensions folder.
		if got, want := s.SupportsExternalExtensions(), s.Name() != "chrome"; got != want {
			t.Errorf("%s SupportsExternalExtensions = %v, want %v", s.Name(), got, want)
		}
	}
}

func TestInstallAndUninstallPerBrowser(t *testing.T) {
	for _, name := range Names() {
		t.Run(name, func(t *testing.T) {
			config, opts := installFixture(t)
			s, err := Get(name)
			if err != nil {
				t.Fatal(err)
			}
			want := manifestFor(config, name, NativeHostName)

			res, err := Install(s, opts)
			if err != nil {
				t.Fatal(err)
			}
			if res.ManifestPath != want || !res.WroteManifest || res.Browser != name {
				t.Fatalf("Install = %+v, want manifest %s written for %s", res, want, name)
			}
			data, err := os.ReadFile(want)
			if err != nil {
				t.Fatal(err)
			}
			var m nativeHostManifest
			if err := json.Unmarshal(data, &m); err != nil {
				t.Fatal(err)
			}
			wantManifest := nativeHostManifest{
				Name:           NativeHostName,
				Description:    "albear vault native messaging bridge (blind relay)",
				Path:           opts.NativeHostPath,
				Type:           "stdio",
				AllowedOrigins: []string{"chrome-extension://" + ChromeExtensionID + "/"},
			}
			if !reflect.DeepEqual(m, wantManifest) {
				t.Fatalf("manifest = %+v, want %+v", m, wantManifest)
			}
			for other := range browserConfigDirs {
				if other != name && exists(t, manifestFor(config, other, NativeHostName)) {
					t.Fatalf("installing %s also wrote %s's manifest", name, other)
				}
			}

			un, err := Uninstall(s, Options{})
			if err != nil {
				t.Fatal(err)
			}
			if !un.RemovedManifest || un.ManifestPath != want {
				t.Fatalf("Uninstall = %+v, want %s removed", un, want)
			}
			if exists(t, want) {
				t.Fatal("manifest still present after uninstall")
			}
			// The folder install created is left alone; only the manifest goes.
			if !exists(t, filepath.Dir(want)) {
				t.Fatal("uninstall removed the NativeMessagingHosts folder")
			}

			un, err = Uninstall(s, Options{})
			if err != nil {
				t.Fatalf("second uninstall: %v", err)
			}
			if un.RemovedManifest {
				t.Fatal("second uninstall reported a removal")
			}
		})
	}
}

func TestInstallPrintOnlyWritesNothing(t *testing.T) {
	config, opts := installFixture(t)
	opts.PrintOnly = true
	res, err := Install(chromiumFamily{name: "helium", configDir: "net.imput.helium"}, opts)
	if err != nil {
		t.Fatal(err)
	}
	if res.WroteManifest || exists(t, filepath.Join(config, "net.imput.helium")) {
		t.Fatal("print-only install wrote to disk")
	}
}

func TestInstallUsesIdentity(t *testing.T) {
	config, opts := installFixture(t)
	opts.Identity = Identity{HostName: "dev.albear.native_dev", ExtensionID: "abcdefghijklmnopabcdefghijklmnop"}
	s, err := Get("brave")
	if err != nil {
		t.Fatal(err)
	}
	res, err := Install(s, opts)
	if err != nil {
		t.Fatal(err)
	}
	want := manifestFor(config, "brave", opts.Identity.HostName)
	if res.ManifestPath != want || res.ExtensionID != opts.Identity.ExtensionID {
		t.Fatalf("Install = %+v, want manifest %s for %s", res, want, opts.Identity.ExtensionID)
	}
	data, err := os.ReadFile(want)
	if err != nil {
		t.Fatal(err)
	}
	var m nativeHostManifest
	if err := json.Unmarshal(data, &m); err != nil {
		t.Fatal(err)
	}
	if m.Name != opts.Identity.HostName || m.AllowedOrigins[0] != "chrome-extension://"+opts.Identity.ExtensionID+"/" {
		t.Fatalf("manifest = %+v", m)
	}
	if exists(t, manifestFor(config, "brave", NativeHostName)) {
		t.Fatal("default-identity manifest written for a custom identity")
	}

	// Uninstall is scoped to the identity it is given.
	if un, err := Uninstall(s, Options{}); err != nil || un.RemovedManifest {
		t.Fatalf("default-identity uninstall = %+v, %v; want nothing removed", un, err)
	}
	if un, err := Uninstall(s, opts); err != nil || !un.RemovedManifest {
		t.Fatalf("custom-identity uninstall = %+v, %v; want manifest removed", un, err)
	}
}

func TestInstallDetected(t *testing.T) {
	tests := []struct {
		name      string
		present   []string // browsers whose config folder exists
		installed []string
		skipped   []string
	}{
		{name: "none", present: nil, installed: nil, skipped: []string{"brave", "chrome", "chromium", "helium"}},
		{name: "helium and chrome", present: []string{"helium", "chrome"}, installed: []string{"chrome", "helium"}, skipped: []string{"brave", "chromium"}},
		{name: "all", present: []string{"brave", "chrome", "chromium", "helium"}, installed: []string{"brave", "chrome", "chromium", "helium"}, skipped: nil},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			config, opts := installFixture(t)
			for _, b := range tt.present {
				if err := os.MkdirAll(filepath.Join(config, browserConfigDirs[b]), 0o755); err != nil {
					t.Fatal(err)
				}
			}
			results, skipped, err := InstallDetected(opts)
			if err != nil {
				t.Fatal(err)
			}
			var gotInstalled, gotSkipped []string
			for _, r := range results {
				gotInstalled = append(gotInstalled, r.Browser)
			}
			for _, s := range skipped {
				gotSkipped = append(gotSkipped, s.Browser)
				if s.ConfigDir != filepath.Join(config, browserConfigDirs[s.Browser]) {
					t.Errorf("%s skipped config dir = %q", s.Browser, s.ConfigDir)
				}
			}
			if !reflect.DeepEqual(gotInstalled, tt.installed) {
				t.Errorf("installed = %v, want %v", gotInstalled, tt.installed)
			}
			if !reflect.DeepEqual(gotSkipped, tt.skipped) {
				t.Errorf("skipped = %v, want %v", gotSkipped, tt.skipped)
			}
			for b := range browserConfigDirs {
				want := false
				for _, i := range tt.installed {
					want = want || i == b
				}
				if got := exists(t, manifestFor(config, b, NativeHostName)); got != want {
					t.Errorf("%s manifest present = %v, want %v", b, got, want)
				}
				// Skipped browsers must not get a config folder created.
				if got := exists(t, filepath.Join(config, browserConfigDirs[b])); got != want {
					t.Errorf("%s config folder present = %v, want %v", b, got, want)
				}
			}
		})
	}
}

func TestUninstallAllRemovesOnlyManifests(t *testing.T) {
	config, opts := installFixture(t)
	for _, b := range []string{"helium", "chrome"} {
		if err := os.MkdirAll(filepath.Join(config, browserConfigDirs[b]), 0o755); err != nil {
			t.Fatal(err)
		}
	}
	// An unrelated manifest from another native host must survive.
	other := filepath.Join(config, browserConfigDirs["helium"], "NativeMessagingHosts", "com.example.other.json")
	if err := os.MkdirAll(filepath.Dir(other), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(other, []byte("{}"), 0o644); err != nil {
		t.Fatal(err)
	}
	if _, _, err := InstallDetected(opts); err != nil {
		t.Fatal(err)
	}

	removed := map[string]bool{}
	for _, s := range All() {
		res, err := Uninstall(s, Options{})
		if err != nil {
			t.Fatalf("uninstall %s: %v", s.Name(), err)
		}
		removed[s.Name()] = res.RemovedManifest
	}
	want := map[string]bool{"brave": false, "chrome": true, "chromium": false, "helium": true}
	if !reflect.DeepEqual(removed, want) {
		t.Fatalf("removed = %v, want %v", removed, want)
	}
	for b := range browserConfigDirs {
		if exists(t, manifestFor(config, b, NativeHostName)) {
			t.Errorf("%s manifest still present", b)
		}
	}
	if !exists(t, other) {
		t.Fatal("uninstall removed another native host's manifest")
	}
}

func TestDetectRequiresDirectory(t *testing.T) {
	config, _ := installFixture(t)
	if err := os.MkdirAll(config, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(config, "chromium"), nil, 0o644); err != nil {
		t.Fatal(err)
	}
	s, err := Get("chromium")
	if err != nil {
		t.Fatal(err)
	}
	if found, err := Detect(s); err != nil || found {
		t.Fatalf("Detect = %v, %v; want false for a regular file", found, err)
	}
}
