// Package native owns the blind relay between the extension and vaultd.
//
// CallerValidator is the strategy interface: each browser family implements
// it, and the host process tries each registered validator against the
// browser's argv. Adding a new browser means one new file with a
// CallerValidator implementation. The native-host manifest is the primary
// trust gate; the validator is defense-in-depth.
package native

import "errors"

var ErrOriginDenied = errors.New("native: extension origin not allowed")

// CallerValidator decides whether a native-messaging host invocation is
// allowed. Each browser family implements it; the host tries each in order
// and rejects if all decline.
type CallerValidator interface {
	// Browser is a short label used in logs and errors.
	Browser() string
	// Validate examines the first CLI argument the browser passes to the
	// host (Chrome: "chrome-extension://<id>/", Firefox: "" or "<addon-id>")
	// and returns nil if the caller is allowed.
	Validate(argv1 string) error
}
