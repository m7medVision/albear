package install

import (
	"encoding/base64"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"

	"github.com/m7medVision/albear/internal/crx"
)

// Package install locations (the Arch package layout). Install looks here
// first, then at the repo's build outputs and next to the running binary.
const (
	PackageCRX          = "/usr/share/albear/albear.crx"
	PackageExtensionDir = "/usr/share/albear/extension"
	PackageNativeHost   = "/usr/bin/vault-native"

	// RepoCRX and RepoExtensionDir are where `make crx` and the extension
	// build write, relative to the repo root.
	RepoCRX          = "extension/albear.crx"
	RepoExtensionDir = "extension/dist"
)

// crxPackage is a verified .crx and what Install needs from it.
type crxPackage struct {
	path    string
	version string
	data    []byte
}

func exeDir() string {
	exe, err := os.Executable()
	if err != nil {
		return ""
	}
	return filepath.Dir(exe)
}

func cwdPath(rel string) string {
	cwd, err := os.Getwd()
	if err != nil {
		return ""
	}
	return filepath.Join(cwd, rel)
}

// resolveNativeHost finds vault-native: the flag, next to this binary (so a
// dev build in the repo pairs with its own dev relay), the working
// directory, the package location, then PATH.
func resolveNativeHost(opts Options) (string, error) {
	if opts.NativeHostPath != "" {
		return cleanExistingFile(opts.NativeHostPath)
	}
	var candidates []string
	if dir := exeDir(); dir != "" {
		candidates = append(candidates, filepath.Join(dir, "vault-native"))
	}
	candidates = append(candidates, cwdPath("vault-native"), opts.system(PackageNativeHost))
	for _, c := range candidates {
		if p, err := cleanExistingFile(c); err == nil {
			return p, nil
		}
	}
	if p, err := exec.LookPath("vault-native"); err == nil {
		return cleanExistingFile(p)
	}
	return "", errors.New("install: could not find vault-native; pass --native-host /path/to/vault-native")
}

// resolveCRX finds the signed extension for id. An explicit path must be a
// valid .crx signed for id. Otherwise the first candidate that is one wins,
// so a dev CLI skips the packaged prod .crx and the reverse; none found is
// not an error (the extension can still be loaded unpacked).
func resolveCRX(opts Options, id Identity) (crxPackage, error) {
	if opts.CRXPath != "" {
		abs, err := filepath.Abs(opts.CRXPath)
		if err != nil {
			return crxPackage{}, err
		}
		return readCRX(abs, id)
	}
	candidates := []string{opts.system(PackageCRX), cwdPath(RepoCRX)}
	if dir := exeDir(); dir != "" {
		candidates = append(candidates, filepath.Join(dir, "albear.crx"))
	}
	for _, c := range candidates {
		if c == "" {
			continue
		}
		if pkg, err := readCRX(c, id); err == nil {
			return pkg, nil
		}
	}
	return crxPackage{}, nil
}

func readCRX(path string, id Identity) (crxPackage, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return crxPackage{}, fmt.Errorf("install: extension package: %w", err)
	}
	pkg, err := crx.Verify(data)
	if err != nil {
		return crxPackage{}, fmt.Errorf("install: %s: %w", path, err)
	}
	if pkg.ID != id.ExtensionID {
		return crxPackage{}, fmt.Errorf("install: %s is signed for extension %s, but this build installs %s (dev and prod extensions differ)", path, pkg.ID, id.ExtensionID)
	}
	m, err := crx.ReadManifest(pkg.Archive)
	if err != nil {
		return crxPackage{}, fmt.Errorf("install: %s: %w", path, err)
	}
	return crxPackage{path: path, version: m.Version, data: data}, nil
}

// resolveExtensionDir finds the unpacked extension. An explicit path only
// has to be a directory. Otherwise the first candidate whose manifest pins
// id wins; none found returns "".
func resolveExtensionDir(opts Options, id Identity) (string, error) {
	if opts.ExtensionDir != "" {
		abs, err := filepath.Abs(opts.ExtensionDir)
		if err != nil {
			return "", err
		}
		st, err := os.Stat(abs)
		if err != nil {
			return "", fmt.Errorf("install: extension directory %s: %w", abs, err)
		}
		if !st.IsDir() {
			return "", fmt.Errorf("install: extension path is not a directory: %s", abs)
		}
		return abs, nil
	}
	candidates := []string{opts.system(PackageExtensionDir), cwdPath(RepoExtensionDir)}
	if dir := exeDir(); dir != "" {
		candidates = append(candidates, filepath.Join(dir, "extension"))
	}
	for _, c := range candidates {
		if c != "" && extensionDirIs(c, id) {
			return c, nil
		}
	}
	return "", nil
}

func extensionDirIs(dir string, id Identity) bool {
	m, err := crx.ReadManifestFile(dir)
	if err != nil || m.Key == "" {
		return false
	}
	der, err := base64.StdEncoding.DecodeString(m.Key)
	return err == nil && crx.ExtensionID(der) == id.ExtensionID
}

func cleanExistingFile(path string) (string, error) {
	if strings.TrimSpace(path) == "" {
		return "", errors.New("install: empty path")
	}
	abs, err := filepath.Abs(path)
	if err != nil {
		return "", err
	}
	st, err := os.Stat(abs)
	if err != nil {
		return "", fmt.Errorf("install: native host %s: %w", abs, err)
	}
	if st.IsDir() {
		return "", fmt.Errorf("install: native host path is a directory: %s", abs)
	}
	if st.Mode().Perm()&0o111 == 0 {
		return "", fmt.Errorf("install: native host is not executable: %s", abs)
	}
	return abs, nil
}
