package main

import (
	"encoding/base64"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/m7medVision/albear/internal/crx"
)

// devKey is the committed dev signing key; it only protects dev builds.
const devKey = "../../extension/keys/dev.pem"

func TestInspectPrintsIDAndVersion(t *testing.T) {
	dir := t.TempDir()
	ext := filepath.Join(dir, "ext")
	if err := os.MkdirAll(ext, 0o755); err != nil {
		t.Fatal(err)
	}
	manifest := `{"manifest_version":3,"name":"albear","version":"1.4.2"}`
	if err := os.WriteFile(filepath.Join(ext, "manifest.json"), []byte(manifest), 0o644); err != nil {
		t.Fatal(err)
	}
	out := filepath.Join(dir, "albear.crx")
	if err := run([]string{"-key", devKey, "-out", out, ext}, &strings.Builder{}); err != nil {
		t.Fatal(err)
	}

	pemData, err := os.ReadFile(devKey)
	if err != nil {
		t.Fatal(err)
	}
	key, err := crx.ParsePrivateKey(pemData)
	if err != nil {
		t.Fatal(err)
	}
	pub, err := crx.PublicKeyDER(key)
	if err != nil {
		t.Fatal(err)
	}

	var got strings.Builder
	if err := run([]string{"-inspect", out}, &got); err != nil {
		t.Fatal(err)
	}
	if want := crx.ExtensionID(pub) + " 1.4.2\n"; got.String() != want {
		t.Fatalf("inspect = %q, want %q", got.String(), want)
	}

	// The unpacked folder has no fixed ID until its manifest pins the key.
	if err := run([]string{"-inspect", ext}, &strings.Builder{}); err == nil {
		t.Fatal("inspect of a folder without a pinned key succeeded")
	}
	pinned := `{"manifest_version":3,"name":"albear","version":"1.4.2","key":"` +
		base64.StdEncoding.EncodeToString(pub) + `"}`
	if err := os.WriteFile(filepath.Join(ext, "manifest.json"), []byte(pinned), 0o644); err != nil {
		t.Fatal(err)
	}
	var dirOut strings.Builder
	if err := run([]string{"-inspect", ext}, &dirOut); err != nil {
		t.Fatal(err)
	}
	if dirOut.String() != got.String() {
		t.Fatalf("folder inspect = %q, crx inspect = %q", dirOut.String(), got.String())
	}
}

func TestInspectRejectsTamperedCRX(t *testing.T) {
	path := filepath.Join(t.TempDir(), "bad.crx")
	if err := os.WriteFile(path, []byte("Cr24 not really"), 0o644); err != nil {
		t.Fatal(err)
	}
	var got strings.Builder
	if err := run([]string{"-inspect", path}, &got); err == nil {
		t.Fatalf("inspect accepted a bad CRX and printed %q", got.String())
	}
}
