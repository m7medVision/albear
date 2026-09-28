// Package install owns local browser integration setup.
//
// BrowserStrategy is the strategy interface for "install the native-host
// manifest for browser X". Chromium-family browsers share one base
// (chromiumFamily, chromium.go) and differ only in their per-user config
// folder, so adding one is a single Register call naming that folder. The
// Install and Uninstall entry points are browser-agnostic.
package install

import (
	"encoding/json"
	"errors"
	"fmt"
	"io/fs"
	"os"
	"os/exec"
	"path/filepath"
	"sort"
	"strings"

	"github.com/m7medVision/albear/internal/version"
)

const (
	// ChromeExtensionID is the prod extension, pinned by PROD_PUBLIC_KEY in
	// extension/build/target.ts.
	ChromeExtensionID = "iblbbooeonkneacnoakpkkpdpehdhdna"
	NativeHostName    = "dev.albear.native"

	// DevExtensionID is the dev extension, pinned by DEV_PUBLIC_KEY in
	// extension/build/target.ts and signed by the committed
	// extension/keys/dev.pem.
	DevExtensionID    = "ohcnnpelgjehhoejpajdkmeejklpaden"
	DevNativeHostName = "dev.albear.native_dev"
)

// Identity is the native host / extension pair a manifest is written for.
// It is an input to Install and Uninstall rather than a property of the
// browser, so every strategy serves any identity.
type Identity struct {
	HostName    string
	ExtensionID string
}

var (
	// DefaultIdentity is the prod identity, used whenever an Identity is
	// left zero.
	DefaultIdentity = Identity{HostName: NativeHostName, ExtensionID: ChromeExtensionID}
	// DevIdentity is the dev extension and its own native host, so the dev
	// and prod extensions can be installed side by side.
	DevIdentity = Identity{HostName: DevNativeHostName, ExtensionID: DevExtensionID}
)

// IdentityFor is the identity a CLI of the given environment installs.
func IdentityFor(env version.Environment) Identity {
	if env == version.Dev {
		return DevIdentity
	}
	return DefaultIdentity
}

func (id Identity) orDefault() Identity {
	if id == (Identity{}) {
		return DefaultIdentity
	}
	return id
}

type Options struct {
	NativeHostPath string
	ExtensionDir   string
	PrintOnly      bool
	Identity       Identity // zero means DefaultIdentity
}

type Result struct {
	Browser        string
	ManifestPath   string
	NativeHostPath string
	ExtensionDir   string
	ExtensionID    string
	WroteManifest  bool
}

// UninstallResult reports what Uninstall removed for one browser.
type UninstallResult struct {
	Browser         string
	ManifestPath    string
	RemovedManifest bool
}

// BrowserStrategy owns everything browser-specific about installing the
// native-host manifest: where the browser keeps its per-user config, where
// the JSON goes, and what shape the allowlist values take.
type BrowserStrategy interface {
	Name() string
	// ConfigDir is the browser's per-user config folder; Detect treats its
	// existence as "this browser is installed".
	ConfigDir() (string, error)
	// NativeHostsDir is the per-user folder the browser reads
	// native-messaging manifests from.
	NativeHostsDir() (string, error)
	// SupportsExternalExtensions reports whether the browser honours a
	// per-user "External Extensions" folder (Google Chrome on Linux does not).
	SupportsExternalExtensions() bool
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
		return nil, fmt.Errorf("install: unknown browser %q (known: %s)", name, strings.Join(Names(), ", "))
	}
	return s, nil
}

// Names returns the registered browser names, sorted.
func Names() []string {
	out := make([]string, 0, len(registry))
	for k := range registry {
		out = append(out, k)
	}
	sort.Strings(out)
	return out
}

// All returns every registered strategy, sorted by name.
func All() []BrowserStrategy {
	out := make([]BrowserStrategy, 0, len(registry))
	for _, n := range Names() {
		out = append(out, registry[n])
	}
	return out
}

// Detect reports whether the browser's per-user config folder exists.
func Detect(s BrowserStrategy) (bool, error) {
	dir, err := s.ConfigDir()
	if err != nil {
		return false, err
	}
	st, err := os.Stat(dir)
	if errors.Is(err, fs.ErrNotExist) {
		return false, nil
	}
	if err != nil {
		return false, err
	}
	return st.IsDir(), nil
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
	id := opts.Identity.orDefault()
	manifestPath, err := manifestPath(s, id)
	if err != nil {
		return Result{}, err
	}
	if err := s.ValidateExtensionID(id.ExtensionID); err != nil {
		return Result{}, err
	}
	result := Result{
		Browser:        s.Name(),
		ManifestPath:   manifestPath,
		NativeHostPath: hostPath,
		ExtensionDir:   extDir,
		ExtensionID:    id.ExtensionID,
	}
	if opts.PrintOnly {
		return result, nil
	}
	data, err := buildManifest(s, id, hostPath)
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

// Skipped is a browser InstallDetected left alone because its per-user
// config folder does not exist.
type Skipped struct {
	Browser   string
	ConfigDir string
}

// InstallDetected runs Install for every registered browser whose config
// folder exists, in name order, and reports the rest as skipped.
func InstallDetected(opts Options) ([]Result, []Skipped, error) {
	var results []Result
	var skipped []Skipped
	for _, s := range All() {
		found, err := Detect(s)
		if err != nil {
			return nil, nil, err
		}
		if !found {
			dir, err := s.ConfigDir()
			if err != nil {
				return nil, nil, err
			}
			skipped = append(skipped, Skipped{Browser: s.Name(), ConfigDir: dir})
			continue
		}
		result, err := Install(s, opts)
		if err != nil {
			return nil, nil, err
		}
		results = append(results, result)
	}
	return results, skipped, nil
}

// Uninstall removes the native-host manifest Install wrote for the given
// strategy and identity (zero means DefaultIdentity). Nothing installed is
// not an error.
func Uninstall(s BrowserStrategy, id Identity) (UninstallResult, error) {
	path, err := manifestPath(s, id.orDefault())
	if err != nil {
		return UninstallResult{}, err
	}
	result := UninstallResult{Browser: s.Name(), ManifestPath: path}
	switch err := os.Remove(path); {
	case err == nil:
		result.RemovedManifest = true
	case !errors.Is(err, fs.ErrNotExist):
		return UninstallResult{}, err
	}
	return result, nil
}

func manifestPath(s BrowserStrategy, id Identity) (string, error) {
	dir, err := s.NativeHostsDir()
	if err != nil {
		return "", err
	}
	return filepath.Join(dir, id.HostName+".json"), nil
}

// buildManifestJSON builds the manifest for the default host name.
func buildManifestJSON(s BrowserStrategy, hostPath, extID string) ([]byte, error) {
	return buildManifest(s, Identity{HostName: NativeHostName, ExtensionID: extID}, hostPath)
}

func buildManifest(s BrowserStrategy, id Identity, hostPath string) ([]byte, error) {
	allowed, err := s.BuildAllowedOrigins(id.ExtensionID)
	if err != nil {
		return nil, err
	}
	return json.MarshalIndent(nativeHostManifest{
		Name:           id.HostName,
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
