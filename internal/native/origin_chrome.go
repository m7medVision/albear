package native

import "strings"

// ChromeValidator enforces the chrome-extension:// origin convention: the
// browser passes its calling extension's origin as argv[1] and we check
// the ID against a pinned allowlist. The native-host manifest is the
// primary gate; this is defense-in-depth.
type ChromeValidator struct {
	AllowedIDs []string
}

func (c ChromeValidator) Browser() string { return "chrome" }

func (c ChromeValidator) Validate(origin string) error {
	id, ok := strings.CutPrefix(origin, "chrome-extension://")
	if !ok {
		return ErrOriginDenied
	}
	id = strings.TrimSuffix(id, "/")
	if len(id) != 32 {
		return ErrOriginDenied
	}
	for _, allowed := range c.AllowedIDs {
		if id == allowed {
			return nil
		}
	}
	return ErrOriginDenied
}
