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
	// CRXPath is the signed extension package; empty means look it up.
	CRXPath   string
	PrintOnly bool
	Identity  Identity // zero means DefaultIdentity
	// SystemRoot prefixes every system-wide path Install reads (package
	// locations, Chrome's external-extension folder); empty means "/".
	// Install never writes under it.
	SystemRoot string
}

func (o Options) system(path string) string {
	if o.SystemRoot == "" {
		return path
	}
	return filepath.Join(o.SystemRoot, path)
}

// ExtensionStatus is how a browser will get the extension itself, beside
// the native-host manifest.
type ExtensionStatus int

const (
	// ExtensionManual: no signed .crx was found, so the extension has to be
	// loaded unpacked from ExtensionDir.
	ExtensionManual ExtensionStatus = iota
	// ExtensionRegistered: the .crx and <id>.json are in the browser's
	// per-user External Extensions folder; it installs on next start.
	ExtensionRegistered
	// ExtensionSystemPresent: Chrome's system external-extension file exists.
	ExtensionSystemPresent
	// ExtensionNeedsSudo: Chrome's system file is missing; SudoCommand
	// creates it.
	ExtensionNeedsSudo
)

type Result struct {
	Browser        string
	ManifestPath   string
	NativeHostPath string
	ExtensionDir   string // "" when no unpacked build was found
	ExtensionID    string
	WroteManifest  bool

	// CRXPath is the signed package installed from; "" when none was found.
	CRXPath          string
	ExtensionVersion string
	Extension        ExtensionStatus
	// ExternalPath is the <id>.json that registers the extension: the
	// per-user one (Registered) or Chrome's system one (SystemPresent,
	// NeedsSudo).
	ExternalPath  string
	WroteExternal bool
	SudoCommand   string
}

// UninstallResult reports what Uninstall removed for one browser.
type UninstallResult struct {
	Browser         string
	ManifestPath    string
	RemovedManifest bool
	// RemovedExternal is whether the per-user External Extensions
	// registration (<id>.json and <id>.crx) was removed.
	RemovedExternal bool
	// SystemExternalPath is Chrome's system external-extension file when it
	// still exists; Uninstall never removes it (it is root-owned).
	SystemExternalPath string
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
	// SystemExtensionsDir is the root-owned external-extensions folder the
	// browser reads when it has no per-user one; "" when not applicable.
	SystemExtensionsDir() string
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

// Install writes the native-host manifest for the given strategy and
// registers the signed extension: in the per-user External Extensions folder
// where the browser has one, otherwise by checking for Chrome's system file.
func Install(s BrowserStrategy, opts Options) (Result, error) {
	id := opts.Identity.orDefault()
	if err := s.ValidateExtensionID(id.ExtensionID); err != nil {
		return Result{}, err
	}
	hostPath, err := resolveNativeHost(opts)
	if err != nil {
		return Result{}, err
	}
	pkg, err := resolveCRX(opts, id)
	if err != nil {
		return Result{}, err
	}
	extDir, err := resolveExtensionDir(opts, id)
	if err != nil {
		return Result{}, err
	}
	if extDir == "" && pkg.path == "" {
		return Result{}, fmt.Errorf("install: could not find the extension %s; build it (make crx) or pass --crx / --extension-dir", id.ExtensionID)
	}
	manifestPath, err := manifestPath(s, id)
	if err != nil {
		return Result{}, err
	}
	result := Result{
		Browser:          s.Name(),
		ManifestPath:     manifestPath,
		NativeHostPath:   hostPath,
		ExtensionDir:     extDir,
		ExtensionID:      id.ExtensionID,
		CRXPath:          pkg.path,
		ExtensionVersion: pkg.version,
	}
	if err := planExtension(s, opts, id, &result); err != nil {
		return Result{}, err
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
	if result.Extension == ExtensionRegistered {
		if err := writeExternal(result.ExternalPath, pkg); err != nil {
			return Result{}, err
		}
		result.WroteExternal = true
	}
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

// Uninstall removes what Install wrote for the given strategy and
// opts.Identity (zero means DefaultIdentity): the native-host manifest and
// the per-user External Extensions registration. Nothing installed is not an
// error. Chrome's root-owned system file is only reported.
func Uninstall(s BrowserStrategy, opts Options) (UninstallResult, error) {
	id := opts.Identity.orDefault()
	path, err := manifestPath(s, id)
	if err != nil {
		return UninstallResult{}, err
	}
	result := UninstallResult{Browser: s.Name(), ManifestPath: path}
	if result.RemovedManifest, err = removeIfExists(path); err != nil {
		return UninstallResult{}, err
	}
	if s.SupportsExternalExtensions() {
		jsonPath, crxPath, err := externalPaths(s, id)
		if err != nil {
			return UninstallResult{}, err
		}
		removedJSON, err := removeIfExists(jsonPath)
		if err != nil {
			return UninstallResult{}, err
		}
		removedCRX, err := removeIfExists(crxPath)
		if err != nil {
			return UninstallResult{}, err
		}
		result.RemovedExternal = removedJSON || removedCRX
	} else if dir := s.SystemExtensionsDir(); dir != "" {
		p := opts.system(filepath.Join(dir, id.ExtensionID+".json"))
		if _, err := os.Stat(p); err == nil {
			result.SystemExternalPath = p
		}
	}
	return result, nil
}

func removeIfExists(path string) (bool, error) {
	switch err := os.Remove(path); {
	case err == nil:
		return true, nil
	case errors.Is(err, fs.ErrNotExist):
		return false, nil
	default:
		return false, err
	}
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
