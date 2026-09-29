package main

import (
	"testing"

	"github.com/m7medVision/albear/internal/install"
	"github.com/m7medVision/albear/internal/version"
)

// Each relay admits only its own environment's extension, so the dev
// extension can never reach the prod daemon through the prod relay.
func TestCallerAllowlistPerEnvironment(t *testing.T) {
	prodOrigin := "chrome-extension://" + install.ChromeExtensionID + "/"
	devOrigin := "chrome-extension://" + install.DevExtensionID + "/"
	tests := []struct {
		env     version.Environment
		allowed string
		denied  string
	}{
		{version.Prod, prodOrigin, devOrigin},
		{version.Dev, devOrigin, prodOrigin},
	}
	for _, tt := range tests {
		t.Run(string(tt.env), func(t *testing.T) {
			validators := callerValidators(chromeIDs(tt.env), nil)
			admits := func(origin string) bool {
				for _, v := range validators {
					if v.Validate(origin) == nil {
						return true
					}
				}
				return false
			}
			if !admits(tt.allowed) {
				t.Errorf("%s relay denies %s", tt.env, tt.allowed)
			}
			if admits(tt.denied) {
				t.Errorf("%s relay admits %s", tt.env, tt.denied)
			}
		})
	}
}
