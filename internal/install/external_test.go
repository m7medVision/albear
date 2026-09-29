package install

import (
	"bytes"
	"crypto/rand"
	"crypto/rsa"
	"encoding/base64"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/m7medVision/albear/internal/crx"
)

var repoRoot = filepath.Join("..", "..")

func devKey(t *testing.T) *rsa.PrivateKey {
	t.Helper()
	data, err := os.ReadFile(filepath.Join(repoRoot, "extension", "keys", "dev.pem"))
	if err != nil {
		t.Fatal(err)
	}
	key, err := crx.ParsePrivateKey(data)
	if err != nil {
		t.Fatal(err)
	}
	return key
}

// writeExtension writes an unpacked extension pinned to key's public key.
func writeExtension(t *testing.T, dir string, key *rsa.PrivateKey, ver string) {
	t.Helper()
	pub, err := crx.PublicKeyDER(key)
	if err != nil {
		t.Fatal(err)
	}
	m, err := json.Marshal(map[string]any{
		"manifest_version": 3,
		"name":             "albear (dev)",
		"version":          ver,
		"key":              base64.StdEncoding.EncodeToString(pub),
	})
	if err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "manifest.json"), m, 0o644); err != nil {
		t.Fatal(err)
	}
}

// writeCRX signs an extension of version ver with key into path.
func writeCRX(t *testing.T, path string, key *rsa.PrivateKey, ver string) []byte {
	t.Helper()
	src := filepath.Join(t.TempDir(), "ext")
	writeExtension(t, src, key, ver)
	archive, err := crx.ZipDir(src)
	if err != nil {
		t.Fatal(err)
	}
	data, err := crx.Pack(archive, key)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, data, 0o644); err != nil {
		t.Fatal(err)
	}
	return data
}

func readPref(t *testing.T, path string) externalPref {
	t.Helper()
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	var p externalPref
	if err := json.Unmarshal(data, &p); err != nil {
		t.Fatal(err)
	}
	return p
}

func externalFor(config, browser, id string) (jsonPath, crxPath string) {
	dir := filepath.Join(config, browserConfigDirs[browser], "External Extensions")
	return filepath.Join(dir, id+".json"), filepath.Join(dir, id+".crx")
}

func TestInstallRegistersExtensionPerBrowser(t *testing.T) {
	key := devKey(t)
	for _, name := range Names() {
		t.Run(name, func(t *testing.T) {
			config, opts := installFixture(t)
			opts.Identity = DevIdentity
			opts.CRXPath = filepath.Join(t.TempDir(), "albear.crx")
			crxData := writeCRX(t, opts.CRXPath, key, "1.2.3")
			s, err := Get(name)
			if err != nil {
				t.Fatal(err)
			}

			res, err := Install(s, opts)
			if err != nil {
				t.Fatal(err)
			}
			if res.CRXPath != opts.CRXPath || res.ExtensionVersion != "1.2.3" {
				t.Fatalf("Install = %+v, want crx %s version 1.2.3", res, opts.CRXPath)
			}
			jsonPath, crxPath := externalFor(config, name, DevExtensionID)

			if name == "chrome" {
				// Nothing per-user, and nothing written system-wide.
				wantSystem := filepath.Join(opts.SystemRoot, "usr/share/google-chrome/extensions", DevExtensionID+".json")
				if res.Extension != ExtensionNeedsSudo || res.ExternalPath != wantSystem || res.WroteExternal {
					t.Fatalf("Install = %+v, want sudo needed for %s", res, wantSystem)
				}
				wantCmd := "sudo mkdir -p " + filepath.Dir(wantSystem) +
					` && printf '%s\n' '{"external_crx":"` + opts.CRXPath + `","external_version":"1.2.3"}' | sudo tee ` + wantSystem + " >/dev/null"
				if res.SudoCommand != wantCmd {
					t.Fatalf("SudoCommand =\n%s\nwant\n%s", res.SudoCommand, wantCmd)
				}
				if exists(t, opts.SystemRoot) || exists(t, filepath.Dir(jsonPath)) {
					t.Fatal("chrome install wrote an external-extension file")
				}
				return
			}

			if res.Extension != ExtensionRegistered || res.ExternalPath != jsonPath || !res.WroteExternal {
				t.Fatalf("Install = %+v, want registered at %s", res, jsonPath)
			}
			if got, want := readPref(t, jsonPath), (externalPref{ExternalCRX: crxPath, ExternalVersion: "1.2.3"}); got != want {
				t.Fatalf("%s = %+v, want %+v", jsonPath, got, want)
			}
			copied, err := os.ReadFile(crxPath)
			if err != nil {
				t.Fatal(err)
			}
			if !bytes.Equal(copied, crxData) {
				t.Fatal("copied .crx differs from the source")
			}

			un, err := Uninstall(s, opts)
			if err != nil {
				t.Fatal(err)
			}
			if !un.RemovedManifest || !un.RemovedExternal {
				t.Fatalf("Uninstall = %+v, want manifest and registration removed", un)
			}
			if exists(t, jsonPath) || exists(t, crxPath) {
				t.Fatal("External Extensions files still present after uninstall")
			}
		})
	}
}

func TestInstallUpdatesExternalVersion(t *testing.T) {
	key := devKey(t)
	config, opts := installFixture(t)
	opts.Identity = DevIdentity
	opts.CRXPath = filepath.Join(t.TempDir(), "albear.crx")
	s, err := Get("helium")
	if err != nil {
		t.Fatal(err)
	}
	jsonPath, crxPath := externalFor(config, "helium", DevExtensionID)

	for _, ver := range []string{"1.2.3", "1.3.0"} {
		data := writeCRX(t, opts.CRXPath, key, ver)
		if _, err := Install(s, opts); err != nil {
			t.Fatal(err)
		}
		if got := readPref(t, jsonPath).ExternalVersion; got != ver {
			t.Fatalf("external_version = %s, want %s", got, ver)
		}
		if copied, err := os.ReadFile(crxPath); err != nil || !bytes.Equal(copied, data) {
			t.Fatalf("copied .crx is not the %s package (%v)", ver, err)
		}
	}
	entries, err := os.ReadDir(filepath.Dir(jsonPath))
	if err != nil {
		t.Fatal(err)
	}
	if len(entries) != 2 {
		t.Fatalf("External Extensions has %d entries, want the .json and .crx only", len(entries))
	}
}

func TestChromeSystemFilePresent(t *testing.T) {
	_, opts := installFixture(t)
	opts.Identity = DevIdentity
	opts.CRXPath = filepath.Join(t.TempDir(), "albear.crx")
	writeCRX(t, opts.CRXPath, devKey(t), "1.2.3")
	system := filepath.Join(opts.SystemRoot, "usr/share/google-chrome/extensions", DevExtensionID+".json")
	if err := os.MkdirAll(filepath.Dir(system), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(system, []byte("{}"), 0o644); err != nil {
		t.Fatal(err)
	}

	res, err := Install(Chrome{}, opts)
	if err != nil {
		t.Fatal(err)
	}
	if res.Extension != ExtensionSystemPresent || res.SudoCommand != "" || res.ExternalPath != system {
		t.Fatalf("Install = %+v, want Chrome set up by %s", res, system)
	}
	un, err := Uninstall(Chrome{}, opts)
	if err != nil {
		t.Fatal(err)
	}
	if un.SystemExternalPath != system || !exists(t, system) {
		t.Fatalf("Uninstall = %+v, want system file reported and left alone", un)
	}
}

func TestInstallDefaultLookup(t *testing.T) {
	key := devKey(t)
	otherKey, err := rsa.GenerateKey(rand.Reader, 2048)
	if err != nil {
		t.Fatal(err)
	}

	tests := []struct {
		name     string
		identity Identity
		crxKey   *rsa.PrivateKey
		wantCRX  bool
	}{
		{"package matches", DevIdentity, key, true},
		// A dev CLI must not pick up another identity's packaged extension.
		{"package is another extension", DevIdentity, otherKey, false},
		{"prod CLI skips dev package", DefaultIdentity, key, false},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			_, opts := installFixture(t)
			root := opts.SystemRoot
			crxPath := filepath.Join(root, "usr/share/albear/albear.crx")
			extDir := filepath.Join(root, "usr/share/albear/extension")
			hostPath := filepath.Join(root, "usr/bin/vault-native")
			writeCRX(t, crxPath, tt.crxKey, "2.0.0")
			writeExtension(t, extDir, tt.crxKey, "2.0.0")
			if err := os.MkdirAll(filepath.Dir(hostPath), 0o755); err != nil {
				t.Fatal(err)
			}
			if err := os.WriteFile(hostPath, []byte("#!/bin/sh\n"), 0o755); err != nil {
				t.Fatal(err)
			}
			opts = Options{Identity: tt.identity, SystemRoot: root}

			res, err := Install(chromiumFamily{name: "helium", configDir: "net.imput.helium", externalExtensions: true}, opts)
			if !tt.wantCRX {
				if err == nil || !strings.Contains(err.Error(), "could not find the extension") {
					t.Fatalf("Install = %+v, %v; want extension not found", res, err)
				}
				return
			}
			if err != nil {
				t.Fatal(err)
			}
			if res.CRXPath != crxPath || res.ExtensionDir != extDir || res.NativeHostPath != hostPath {
				t.Fatalf("Install = %+v, want package locations under %s", res, root)
			}
			if res.Extension != ExtensionRegistered || res.ExtensionVersion != "2.0.0" {
				t.Fatalf("Install = %+v, want registered 2.0.0", res)
			}
		})
	}
}

func TestInstallCRXErrors(t *testing.T) {
	otherKey, err := rsa.GenerateKey(rand.Reader, 2048)
	if err != nil {
		t.Fatal(err)
	}
	tests := []struct {
		name  string
		write func(t *testing.T, path string)
		want  string
	}{
		{"missing", func(*testing.T, string) {}, "no such file"},
		{"not a crx", func(t *testing.T, p string) {
			if err := os.WriteFile(p, []byte("PK\x03\x04"), 0o644); err != nil {
				t.Fatal(err)
			}
		}, "not a CRX"},
		{"other extension", func(t *testing.T, p string) { writeCRX(t, p, otherKey, "1.0.0") }, "is signed for extension"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			config, opts := installFixture(t)
			opts.Identity = DevIdentity
			opts.CRXPath = filepath.Join(t.TempDir(), "albear.crx")
			tt.write(t, opts.CRXPath)
			s, err := Get("chromium")
			if err != nil {
				t.Fatal(err)
			}
			if _, err := Install(s, opts); err == nil || !strings.Contains(err.Error(), tt.want) {
				t.Fatalf("Install error = %v, want %q", err, tt.want)
			}
			if exists(t, filepath.Join(config, "chromium")) {
				t.Fatal("failed install wrote files")
			}
		})
	}
}

func TestInstallPrintOnlyWritesNoExtension(t *testing.T) {
	config, opts := installFixture(t)
	opts.Identity = DevIdentity
	opts.PrintOnly = true
	opts.CRXPath = filepath.Join(t.TempDir(), "albear.crx")
	writeCRX(t, opts.CRXPath, devKey(t), "1.2.3")
	s, err := Get("brave")
	if err != nil {
		t.Fatal(err)
	}
	res, err := Install(s, opts)
	if err != nil {
		t.Fatal(err)
	}
	jsonPath, _ := externalFor(config, "brave", DevExtensionID)
	if res.Extension != ExtensionRegistered || res.ExternalPath != jsonPath || res.WroteExternal {
		t.Fatalf("Install = %+v, want the registration planned but not written", res)
	}
	if exists(t, config) {
		t.Fatal("print-only install wrote to disk")
	}
}
