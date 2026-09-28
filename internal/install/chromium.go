package install

import (
	"fmt"
	"os"
	"path/filepath"
	"runtime"
)

// chromiumFamily is the shared BrowserStrategy for Chromium-based browsers
// on Linux. They all keep a per-user config folder under $XDG_CONFIG_HOME
// (fallback ~/.config) with a NativeMessagingHosts folder inside it and use
// chrome-extension:// origins, so a browser is described by its folder name
// alone. Linux-only today; the OS gate lives here, not in the generic Install.
type chromiumFamily struct {
	name      string
	configDir string // relative to the XDG config home
	// externalExtensions is whether the browser honours a per-user
	// "External Extensions" folder inside its config folder.
	externalExtensions bool
	// systemExtensionsDir is the root-owned external-extensions folder, for
	// a browser with no per-user one.
	systemExtensionsDir string
}

func (b chromiumFamily) Name() string                     { return b.name }
func (b chromiumFamily) SupportsExternalExtensions() bool { return b.externalExtensions }
func (b chromiumFamily) SystemExtensionsDir() string      { return b.systemExtensionsDir }

func (b chromiumFamily) ConfigDir() (string, error) {
	if runtime.GOOS != "linux" {
		return "", fmt.Errorf("%s install: only Linux current-user installs are supported", b.name)
	}
	config := os.Getenv("XDG_CONFIG_HOME")
	if config == "" {
		home, err := os.UserHomeDir()
		if err != nil {
			return "", err
		}
		config = filepath.Join(home, ".config")
	}
	return filepath.Join(config, b.configDir), nil
}

func (b chromiumFamily) NativeHostsDir() (string, error) {
	config, err := b.ConfigDir()
	if err != nil {
		return "", err
	}
	return filepath.Join(config, "NativeMessagingHosts"), nil
}

// ManifestPath is where Install writes the manifest for DefaultIdentity.
func (b chromiumFamily) ManifestPath() (string, error) {
	return manifestPath(b, DefaultIdentity)
}

func (chromiumFamily) BuildAllowedOrigins(extensionID string) ([]string, error) {
	return []string{"chrome-extension://" + extensionID + "/"}, nil
}

func (b chromiumFamily) ValidateExtensionID(id string) error {
	if len(id) != 32 {
		return fmt.Errorf("%s install: extension ID must be 32 characters", b.name)
	}
	for _, r := range id {
		if r < 'a' || r > 'p' {
			return fmt.Errorf("%s install: extension ID contains invalid character %q", b.name, r)
		}
	}
	return nil
}

func init() {
	Register(chromiumFamily{name: "chromium", configDir: "chromium", externalExtensions: true})
	Register(chromiumFamily{name: "brave", configDir: filepath.Join("BraveSoftware", "Brave-Browser"), externalExtensions: true})
	Register(chromiumFamily{name: "helium", configDir: "net.imput.helium", externalExtensions: true})
}
