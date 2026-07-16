// vault-native is the Chrome Native Messaging bridge: a blind relay between
// the extension and vaultd. It validates the calling extension's exact ID,
// then forwards opaque Noise frames it cannot decrypt (PRD 11.3).
package main

import (
	"fmt"
	"os"
	"strings"

	"github.com/m7medVision/albear/internal/infrastructure/system"
	"github.com/m7medVision/albear/internal/install"
	"github.com/m7medVision/albear/internal/native"
	"github.com/m7medVision/albear/internal/version"
)

// productionChromeIDs is the exact allowlist baked into release builds.
// ALBEAR_EXTENSION_IDS overrides it for development only.
var productionChromeIDs = []string{install.ChromeExtensionID}

// productionFirefoxIDs is empty: no Firefox build yet. When Firefox lands,
// append the addon ID here and add a native.FirefoxValidator to the
// candidate list in run().
var productionFirefoxIDs []string

func main() {
	// Version print only: the relay stays purely offline. Browsers pass an
	// extension origin as os.Args[1], never these flags, so there is no clash.
	if len(os.Args) > 1 && (os.Args[1] == "--version" || os.Args[1] == "version") {
		fmt.Println("vault-native", version.Version)
		return
	}
	if err := run(); err != nil {
		fmt.Fprintln(os.Stderr, "vault-native:", err)
		os.Exit(1)
	}
}

func run() error {
	chromeIDs := productionChromeIDs
	if env := os.Getenv("ALBEAR_EXTENSION_IDS"); env != "" {
		chromeIDs = strings.Split(env, ",")
	}

	candidates := callerValidators(chromeIDs, productionFirefoxIDs)
	if len(candidates) == 0 {
		return fmt.Errorf("vault-native: no caller allowlist configured")
	}
	origin := ""
	if len(os.Args) > 1 {
		origin = os.Args[1]
	}
	for _, v := range candidates {
		if err := v.Validate(origin); err == nil {
			paths, err := system.ResolvePaths()
			if err != nil {
				return err
			}
			return native.Relay(os.Stdin, os.Stdout, paths.Socket())
		}
	}
	return native.ErrOriginDenied
}

// callerValidators builds the strategy chain. Adding a new browser is one
// append here + one new native.<Browser>Validator type in its own file.
func callerValidators(chromeIDs, firefoxIDs []string) []native.CallerValidator {
	out := []native.CallerValidator{}
	if len(chromeIDs) > 0 {
		out = append(out, native.ChromeValidator{AllowedIDs: chromeIDs})
	}
	if len(firefoxIDs) > 0 {
		// future: out = append(out, native.FirefoxValidator{AllowedIDs: firefoxIDs})
	}
	return out
}
