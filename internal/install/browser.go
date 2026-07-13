// Package install owns local browser integration setup.
//
// BrowserStrategy is the strategy interface for "install the native-host
// manifest for browser X". Adding a new browser means dropping a new file
// in this package with a struct that implements the interface and calling
// Register from its own init(). The Install entry point is browser-agnostic.
package install

import (
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"sort"
	"strings"
)

const (
	ChromeExtensionID = "iblbbooeonkneacnoakpkkpdpehdhdna"
	NativeHostName    = "dev.albear.native"
)

type Options struct {
	NativeHostPath string
	ExtensionDir   string
	PrintOnly      bool
}

type Result struct {
	ManifestPath   string
	NativeHostPath string
	ExtensionDir   string
	ExtensionID    string
	WroteManifest  bool
}

// BrowserStrategy owns everything browser-specific about installing the
// native-host manifest: where the JSON goes, what the allowlist key is
// called, what shape the allowed values take, and the pinned extension ID.
type BrowserStrategy interface {
	Name() string
	ExtensionID() string
	ManifestPath() (string, error)
	AllowedOriginsKey() string
	BuildAllowedOrigins(extensionID string) ([]string, error)
	ValidateExtensionID(id string) error
}

type nativeHostManifest struct {
	Name           string   `json:"name"`
	Description    string   `json:"description"`
	Path           string   `json:"path"`
	Type           string   `json:"type"`
	AllowedOrigins []string `json:"allowed_origins"`
}

var registry = map[string]BrowserStrategy{}

// Register adds a strategy to the package registry. Each strategy calls
// this from its own init().
func Register(s BrowserStrategy) { registry[s.Name()] = s }

// Get returns the strategy registered under name, or an error listing the
// known names.
func Get(name string) (BrowserStrategy, error) {
	s, ok := registry[name]
	if !ok {
		return nil, fmt.Errorf("install: unknown browser %q (known: %s)", name, strings.Join(knownNames(), ", "))
	}
	return s, nil
}

func knownNames() []string {
	out := make([]string, 0, len(registry))
	for k := range registry {
		out = append(out, k)
	}
	sort.Strings(out)
	return out
}

// Install writes the native-host manifest for the given strategy.
func Install(s BrowserStrategy, opts Options) (Result, error) {
	hostPath, err := resolveNativeHost(opts.NativeHostPath)
	if err != nil {
		return Result{}, err
	}
	extDir, err := resolveExtensionDir(opts.ExtensionDir)
	if err != nil {
		return Result{}, err
	}
	manifestPath, err := s.ManifestPath()
	if err != nil {
		return Result{}, err
	}
	extID := s.ExtensionID()
	if err := s.ValidateExtensionID(extID); err != nil {
		return Result{}, err
	}
	result := Result{
		ManifestPath:   manifestPath,
		NativeHostPath: hostPath,
		ExtensionDir:   extDir,
		ExtensionID:    extID,
	}
	if opts.PrintOnly {
		return result, nil
	}
	data, err := buildManifestJSON(s, hostPath, extID)
	if err != nil {
		return Result{}, err
	}
	if err := os.MkdirAll(filepath.Dir(manifestPath), 0o755); err != nil {
		return Result{}, err
	}
	if err := os.WriteFile(manifestPath, data, 0o644); err != nil {
		return Result{}, err
	}
	result.WroteManifest = true
	return result, nil
}

func buildManifestJSON(s BrowserStrategy, hostPath, extID string) ([]byte, error) {
	allowed, err := s.BuildAllowedOrigins(extID)
	if err != nil {
		return nil, err
	}
	return json.MarshalIndent(nativeHostManifest{
		Name:           NativeHostName,
		Description:    "albear vault native messaging bridge (blind relay)",
		Path:           hostPath,
		Type:           "stdio",
		AllowedOrigins: allowed,
	}, "", "  ")
}

func resolveNativeHost(path string) (string, error) {
	if path != "" {
		return cleanExistingFile(path)
	}
	if exe, err := os.Executable(); err == nil {
		if p, err := cleanExistingFile(filepath.Join(filepath.Dir(exe), "vault-native")); err == nil {
			return p, nil
		}
	}
	if cwd, err := os.Getwd(); err == nil {
		if p, err := cleanExistingFile(filepath.Join(cwd, "vault-native")); err == nil {
			return p, nil
		}
	}
	if p, err := exec.LookPath("vault-native"); err == nil {
		return cleanExistingFile(p)
	}
	return "", errors.New("install: could not find vault-native; pass --native-host /path/to/vault-native")
}

func resolveExtensionDir(path string) (string, error) {
	if path == "" {
		path = filepath.Join("extension", "dist")
	}
	abs, err := filepath.Abs(path)
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
