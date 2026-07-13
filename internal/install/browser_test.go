package install

import (
	"encoding/json"
	"os"
	"path/filepath"
	"testing"
)

func TestChromeStrategyManifestJSON(t *testing.T) {
	s := Chrome{}
	data, err := buildManifestJSON(s, "/opt/albear/vault-native", ChromeExtensionID)
	if err != nil {
		t.Fatal(err)
	}
	var got nativeHostManifest
	if err := json.Unmarshal(data, &got); err != nil {
		t.Fatal(err)
	}
	if got.Name != NativeHostName {
		t.Fatalf("name = %q", got.Name)
	}
	if got.Path != "/opt/albear/vault-native" {
		t.Fatalf("path = %q", got.Path)
	}
	if got.Type != "stdio" {
		t.Fatalf("type = %q", got.Type)
	}
	wantOrigin := "chrome-extension://" + ChromeExtensionID + "/"
	if len(got.AllowedOrigins) != 1 || got.AllowedOrigins[0] != wantOrigin {
		t.Fatalf("allowed origins = %#v, want %q", got.AllowedOrigins, wantOrigin)
	}
}

func TestChromeValidateExtensionID(t *testing.T) {
	s := Chrome{}
	if err := s.ValidateExtensionID(ChromeExtensionID); err != nil {
		t.Fatal(err)
	}
	for _, id := range []string{
		"short",
		"iblbbooeonkneacnoakpkkpdpehdhdnq",
		"IBLBBOOEONKNEACNOAKPKKPDPEHDHDNA",
	} {
		if err := s.ValidateExtensionID(id); err == nil {
			t.Fatalf("ValidateExtensionID(%q) succeeded", id)
		}
	}
}

func TestChromeManifestPathUsesXDGConfigHome(t *testing.T) {
	t.Setenv("XDG_CONFIG_HOME", filepath.Join(t.TempDir(), "config"))
	s := Chrome{}
	got, err := s.ManifestPath()
	if err != nil {
		t.Fatal(err)
	}
	want := filepath.Join(
		os.Getenv("XDG_CONFIG_HOME"),
		"google-chrome",
		"NativeMessagingHosts",
		NativeHostName+".json",
	)
	if got != want {
		t.Fatalf("manifest path = %q, want %q", got, want)
	}
}

func TestResolveNativeHostRequiresExecutableFile(t *testing.T) {
	path := filepath.Join(t.TempDir(), "vault-native")
	if err := os.WriteFile(path, []byte("not executable"), 0o644); err != nil {
		t.Fatal(err)
	}
	if _, err := resolveNativeHost(path); err == nil {
		t.Fatal("resolveNativeHost succeeded for non-executable file")
	}
}

func TestRegistryHasChrome(t *testing.T) {
	s, err := Get("chrome")
	if err != nil {
		t.Fatal(err)
	}
	if s.Name() != "chrome" {
		t.Fatalf("name = %q", s.Name())
	}
	if _, err := Get("firefox"); err == nil {
		t.Fatal("Get(firefox) succeeded; registry should be chrome-only today")
	}
}

func TestInstallWritesManifest(t *testing.T) {
	dir := t.TempDir()
	hostPath := filepath.Join(dir, "vault-native")
	if err := os.WriteFile(hostPath, []byte("#!/bin/sh\n"), 0o755); err != nil {
		t.Fatal(err)
	}
	extDir := filepath.Join(dir, "ext")
	if err := os.MkdirAll(extDir, 0o755); err != nil {
		t.Fatal(err)
	}
	t.Setenv("XDG_CONFIG_HOME", filepath.Join(dir, "config"))

	res, err := Install(Chrome{}, Options{NativeHostPath: hostPath, ExtensionDir: extDir})
	if err != nil {
		t.Fatal(err)
	}
	if !res.WroteManifest {
		t.Fatal("manifest not written")
	}
	data, err := os.ReadFile(res.ManifestPath)
	if err != nil {
		t.Fatal(err)
	}
	var m nativeHostManifest
	if err := json.Unmarshal(data, &m); err != nil {
		t.Fatal(err)
	}
	if m.Path != hostPath {
		t.Fatalf("path in manifest = %q, want %q", m.Path, hostPath)
	}
}
