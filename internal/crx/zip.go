package crx

import (
	"archive/zip"
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"io/fs"
	"os"
	"path/filepath"
)

// Manifest is the part of an extension's manifest.json install cares about.
type Manifest struct {
	Name    string `json:"name"`
	Version string `json:"version"`
	// Key is the base64 DER public key pinning the extension ID.
	Key string `json:"key"`
}

// ZipDir archives an unpacked extension folder, with paths relative to dir.
// Entries are written in walk (lexical) order.
func ZipDir(dir string) ([]byte, error) {
	var buf bytes.Buffer
	zw := zip.NewWriter(&buf)
	err := filepath.WalkDir(dir, func(path string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if d.IsDir() {
			return nil
		}
		if !d.Type().IsRegular() {
			return fmt.Errorf("crx: %s is not a regular file", path)
		}
		rel, err := filepath.Rel(dir, path)
		if err != nil {
			return err
		}
		w, err := zw.CreateHeader(&zip.FileHeader{Name: filepath.ToSlash(rel), Method: zip.Deflate})
		if err != nil {
			return err
		}
		f, err := os.Open(path)
		if err != nil {
			return err
		}
		defer f.Close()
		_, err = io.Copy(w, f)
		return err
	})
	if err != nil {
		return nil, err
	}
	if err := zw.Close(); err != nil {
		return nil, err
	}
	return buf.Bytes(), nil
}

// ReadManifest reads manifest.json from a ZIP archive.
func ReadManifest(archive []byte) (Manifest, error) {
	zr, err := zip.NewReader(bytes.NewReader(archive), int64(len(archive)))
	if err != nil {
		return Manifest{}, fmt.Errorf("crx: archive: %w", err)
	}
	f, err := zr.Open("manifest.json")
	if err != nil {
		return Manifest{}, fmt.Errorf("crx: archive: %w", err)
	}
	defer f.Close()
	return decodeManifest(f)
}

// ReadManifestFile reads an unpacked extension's manifest.json.
func ReadManifestFile(dir string) (Manifest, error) {
	f, err := os.Open(filepath.Join(dir, "manifest.json"))
	if err != nil {
		return Manifest{}, err
	}
	defer f.Close()
	return decodeManifest(f)
}

func decodeManifest(r io.Reader) (Manifest, error) {
	var m Manifest
	if err := json.NewDecoder(r).Decode(&m); err != nil {
		return Manifest{}, fmt.Errorf("crx: manifest.json: %w", err)
	}
	if m.Version == "" {
		return Manifest{}, errors.New("crx: manifest.json has no version")
	}
	return m, nil
}
