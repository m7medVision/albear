package install

import (
	"fmt"
	"os"
	"path/filepath"
	"runtime"
)

// Chrome is the BrowserStrategy for Google Chrome (and Chromium-family
// browsers sharing the same NativeMessagingHosts directory layout).
// Linux-only today; the OS gate lives here, not in the generic Install.
type Chrome struct{}

func (Chrome) Name() string { return "chrome" }
func (Chrome) ExtensionID() string { return ChromeExtensionID }

func (Chrome) ManifestPath() (string, error) {
	if runtime.GOOS != "linux" {
		return "", fmt.Errorf("chrome install: only Linux current-user installs are supported")
	}
	config := os.Getenv("XDG_CONFIG_HOME")
	if config == "" {
		home, err := os.UserHomeDir()
		if err != nil {
			return "", err
		}
		config = filepath.Join(home, ".config")
	}
	return filepath.Join(config, "google-chrome", "NativeMessagingHosts", NativeHostName+".json"), nil
}

func (Chrome) AllowedOriginsKey() string { return "allowed_origins" }

func (Chrome) BuildAllowedOrigins(extensionID string) ([]string, error) {
	return []string{"chrome-extension://" + extensionID + "/"}, nil
}

func (Chrome) ValidateExtensionID(id string) error {
	if len(id) != 32 {
		return fmt.Errorf("chrome install: extension ID must be 32 characters")
	}
	for _, r := range id {
		if r < 'a' || r > 'p' {
			return fmt.Errorf("chrome install: extension ID contains invalid character %q", r)
		}
	}
	return nil
}

func init() { Register(Chrome{}) }
