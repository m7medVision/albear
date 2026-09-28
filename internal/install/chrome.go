package install

// chrome describes Google Chrome within the Chromium family. Chrome on
// Linux has no per-user External Extensions folder.
var chrome = chromiumFamily{name: "chrome", configDir: "google-chrome"}

// Chrome is the BrowserStrategy for Google Chrome. It stays a named type
// (rather than a bare chromiumFamily value like the other browsers) so
// callers constructing Chrome{} keep working; every method delegates to
// the shared Chromium-family base.
type Chrome struct{}

func (Chrome) Name() string                        { return chrome.Name() }
func (Chrome) ConfigDir() (string, error)          { return chrome.ConfigDir() }
func (Chrome) NativeHostsDir() (string, error)     { return chrome.NativeHostsDir() }
func (Chrome) ManifestPath() (string, error)       { return chrome.ManifestPath() }
func (Chrome) SupportsExternalExtensions() bool    { return chrome.SupportsExternalExtensions() }
func (Chrome) ValidateExtensionID(id string) error { return chrome.ValidateExtensionID(id) }

func (Chrome) BuildAllowedOrigins(extensionID string) ([]string, error) {
	return chrome.BuildAllowedOrigins(extensionID)
}

func init() { Register(Chrome{}) }
