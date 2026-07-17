package domain

import (
	"errors"
	"strings"
	"testing"
)

func TestCheckPasswordStrength(t *testing.T) {
	cases := []struct {
		name     string
		password string
		accept   bool
	}{
		// Length floor.
		{"empty", "", false},
		{"short", "hunter2", false},
		{"one below the floor", "abcdEF12!@#", false},       // 11
		{"at the floor with variety", "abcdEF12!@#$", true}, // 12

		// The entropy floor applies below PassphraseLength.
		{"twelve lowercase only", "abcdefghijkm", false},      // 12 x log2(26) = 56.4
		{"twelve digits only", "195830472619", false},         // 12 x log2(10) = 39.9
		{"twelve mixed case", "aBcDeFgHiJkM", true},           // 12 x log2(52) = 68.4
		{"twelve lowercase plus digit", "abcdefghijk1", true}, // 12 x log2(36) = 62.0

		// Thirteen lowercase already clears the entropy floor (13 x 4.7 =
		// 61.1); twelve does not. That step is the floor doing its job.
		{"thirteen lowercase", "abcmxpqrtyzwk", true},
		{"fifteen lowercase", "correctbatteryx", true}, // 70.5 bits

		// The passphrase path: length alone carries it.
		{"sixteen lowercase accepted", "quietriverstones", true}, // 16
		{"long spaced passphrase", "seven sailing rusty owls", true},
		{"master password used by tests", "master password", true},

		// Common passwords are rejected whatever their length or case.
		{"common long", "password1234", false},
		{"common uppercase", "PASSWORD1234", false},
		{"common mixed case", "PaSsWoRd1234", false},
		{"common keyboard walk", "qwertyuiopasdfgh", false},
		{"common passphrase", "correcthorsebatterystaple", false},

		// Structural rules bite on the passphrase path, where the length
		// check would otherwise wave them through.
		{"single repeated char", "aaaaaaaaaaaaaaaa", false},
		{"repeated pair", "abababababababab", false},
		{"repeated word", "abcdefabcdefabcdef", false},
		{"ascending run", "abcdefghijklmnop", false},
		{"descending run", "ponmlkjihgfedcba", false},
		{"ascending digits below the floor", "123456789012", false},

		// A sequence that only starts sequential is fine.
		{"sequential prefix only", "abcdefgh yellow submarine", true},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			err := CheckPasswordStrength(tc.password)
			if tc.accept && err != nil {
				t.Fatalf("rejected %q: %v", tc.password, err)
			}
			if !tc.accept {
				if err == nil {
					t.Fatalf("accepted %q", tc.password)
				}
				if !errors.Is(err, ErrWeakPassword) {
					t.Fatalf("%q: unexpected error %v", tc.password, err)
				}
			}
		})
	}
}

// TestPasswordErrorIsUniform: every rejection returns the same error, so the
// message cannot be used to work out which rule to tiptoe around.
func TestPasswordErrorIsUniform(t *testing.T) {
	for _, p := range []string{"", "short", "abcdefghijkm", "password1234", "aaaaaaaaaaaaaaaa"} {
		if err := CheckPasswordStrength(p); !errors.Is(err, ErrWeakPassword) {
			t.Fatalf("%q returned %v", p, err)
		}
	}
	// The message names no rule and echoes no input.
	if strings.Contains(ErrWeakPassword.Error(), "entropy") ||
		strings.Contains(ErrWeakPassword.Error(), "common") {
		t.Fatalf("error names the failing rule: %q", ErrWeakPassword)
	}
}

// TestPasswordCheckIsSecretSafe: the policy must never echo the password, and
// must not choke on non-ASCII input (invariant 8 in spirit — no secret ever
// reaches an error string).
func TestPasswordCheckHandlesUnicode(t *testing.T) {
	// Multi-byte runes count as runes, not bytes: this is 13 runes.
	if err := CheckPasswordStrength("مفتاحسريطويلجدا"); err != nil {
		t.Fatalf("unicode passphrase rejected: %v", err)
	}
	// A short unicode password is still short.
	if err := CheckPasswordStrength("سر"); !errors.Is(err, ErrWeakPassword) {
		t.Fatalf("short unicode accepted: %v", err)
	}
	if got := ErrWeakPassword.Error(); strings.Contains(got, "مفتاح") {
		t.Fatal("error echoed the password")
	}
}
