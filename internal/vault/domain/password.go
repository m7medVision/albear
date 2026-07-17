package domain

import (
	"errors"
	"math"
	"strings"
	"unicode"
)

// Master-password policy (design G3). The vault key is only ever as strong as
// this password: Argon2id raises the cost of each guess, it does not shrink
// the guess space. The thresholds live here as named constants so they can be
// audited against the design document.
const (
	// MinPasswordLength is the floor for any master password.
	MinPasswordLength = 12

	// PassphraseLength is the length at or above which a password is accepted
	// on length alone. Long passphrases are the behaviour we want to
	// encourage, and the character-class estimate below punishes them for
	// being all-lowercase when their length already carries the work.
	PassphraseLength = 16

	// MinPasswordEntropyBits is the estimated floor for passwords shorter than
	// PassphraseLength.
	MinPasswordEntropyBits = 60
)

// ErrWeakPassword is returned for every policy failure. It is deliberately a
// single error rather than per-rule feedback: naming the rule that tripped
// invites nudging a bad password until it squeaks past, instead of choosing a
// better one.
var ErrWeakPassword = errors.New("vault: master password does not meet the strength policy")

// CheckPasswordStrength applies the master-password policy. It is pure —
// standard library only, no clock, no I/O — so it can live in the domain
// (invariant 3) and be called from both init and password change.
func CheckPasswordStrength(password string) error {
	runes := []rune(password)
	if len(runes) < MinPasswordLength {
		return ErrWeakPassword
	}
	if isCommonPassword(password) {
		return ErrWeakPassword
	}
	// These two would otherwise ride the passphrase path on length alone.
	if isRepeatedUnit(runes) || isSequentialRun(runes) {
		return ErrWeakPassword
	}
	if len(runes) < PassphraseLength && entropyBits(runes) < MinPasswordEntropyBits {
		return ErrWeakPassword
	}
	return nil
}

// entropyBits estimates length x log2(pool), where pool is the combined size
// of the character classes the password actually draws from.
//
// This is an upper bound on strength, not a measurement: it assumes each
// character was chosen uniformly at random, which a human-chosen password
// never is ("Password1234!" scores far above its real worth). It is here to
// reject the obviously thin, not to certify the rest — the common-password
// list and the structural rules cover what it overestimates.
func entropyBits(runes []rune) float64 {
	var lower, upper, digit, symbol, other bool
	for _, r := range runes {
		switch {
		case unicode.IsLower(r):
			lower = true
		case unicode.IsUpper(r):
			upper = true
		case unicode.IsDigit(r):
			digit = true
		case r <= unicode.MaxASCII:
			symbol = true
		default:
			other = true
		}
	}
	pool := 0
	if lower {
		pool += 26
	}
	if upper {
		pool += 26
	}
	if digit {
		pool += 10
	}
	if symbol {
		pool += 33 // printable ASCII punctuation and space
	}
	if other {
		pool += 100 // conservative allowance for anything non-ASCII
	}
	if pool == 0 {
		return 0
	}
	return float64(len(runes)) * math.Log2(float64(pool))
}

// isRepeatedUnit reports whether the password is one unit repeated end to end
// ("aaaaaaaaaaaa", "abababababab", "abcdefabcdef"). Such a password is only as
// strong as its unit, but every length-based rule would credit it in full.
func isRepeatedUnit(runes []rune) bool {
	n := len(runes)
	for unit := 1; unit <= n/2; unit++ {
		if n%unit != 0 {
			continue
		}
		if repeatsEvery(runes, unit) {
			return true
		}
	}
	return false
}

func repeatsEvery(runes []rune, unit int) bool {
	for i := unit; i < len(runes); i++ {
		if runes[i] != runes[i-unit] {
			return false
		}
	}
	return true
}

// isSequentialRun reports whether the whole password walks consecutive code
// points in one direction ("abcdefghijklmnop", "ponmlkjihgfedcba"). Short
// sequences already fail the entropy floor; this catches the long ones that
// would otherwise be accepted as passphrases.
func isSequentialRun(runes []rune) bool {
	if len(runes) < 2 {
		return false
	}
	step := runes[1] - runes[0]
	if step != 1 && step != -1 {
		return false
	}
	for i := 2; i < len(runes); i++ {
		if runes[i]-runes[i-1] != step {
			return false
		}
	}
	return true
}

func isCommonPassword(password string) bool {
	_, found := commonPasswords[strings.ToLower(password)]
	return found
}

// commonPasswords is a small embedded set drawn from the top of public breach
// corpora. MinPasswordLength already rejects most of the classics on length,
// so this leans towards the long ones that would otherwise pass: keyboard
// walks, doubled words, and the usual "add 1234 to make it strong" pattern.
// A few short entries are kept so the list still bites if the length floor
// ever moves. Matching is exact and case-insensitive — this is a deny list for
// the notorious, not a substring heuristic that would reject any password
// containing "dragon".
var commonPasswords = map[string]struct{}{}

func init() {
	for _, p := range []string{
		// Long-enough to clear MinPasswordLength on their own.
		"123456789012", "1234567890123", "12345678901234", "123456789012345",
		"1234567890123456", "111111111111", "000000000000", "121212121212",
		"password1234", "password12345", "password123456", "passw0rd1234",
		"passwordpassword", "password1!", "p@ssw0rd1234", "p@ssword1234",
		"qwertyuiop12", "qwertyuiop123", "qwertyuiop1234", "qwertyuiopasdfgh",
		"qwertyuiopasd", "qazwsxedcrfv", "qazwsxedcrfvtgb", "1qaz2wsx3edc",
		"1qaz2wsx3edc4rfv", "zaq12wsxcde3", "q1w2e3r4t5y6", "1q2w3e4r5t6y",
		"asdfghjkl123", "asdfghjklzxcvbnm", "zxcvbnm12345", "zxcvbnmasdfghjkl",
		"administrator", "administrator1", "letmein12345", "letmeinplease",
		"welcome12345", "welcome123456", "welcometoyou", "changeme1234",
		"iloveyou1234", "iloveyou12345", "iloveyouforever", "iloveyousomuch",
		"monkey123456", "dragon123456", "football1234", "baseball1234",
		"basketball12", "superman1234", "batman123456", "sunshine1234",
		"princess1234", "princess12345", "michael12345", "jennifer1234",
		"trustno1", "trustnoone12", "mypassword123", "mynameisnobody",
		"secretpassword", "supersecret1", "supersecretpassword",
		"masterpassword", "masterpassword1", "thisismypassword",
		"thisisapassword", "correcthorsebatterystaple", "letmeinnow123",
		"whatever1234", "nothing12345", "computer1234", "internet1234",
		"starwars1234", "pokemon12345", "liverpool123", "manchester12",
		"chocolate123", "butterfly123", "shadow123456", "jordan123456",
		"harleydavidson", "loveme123456", "hellohello12", "helloworld12",
		"abcd1234efgh", "abcdefgh1234", "abc123abc123", "a1b2c3d4e5f6",
		"passw0rd!123", "welcome1!2345", "qwerty1234567", "michelle1234",
		"december1234", "september123", "temporary1234", "default12345",
		"guestguest12", "testtest1234", "vaultpassword", "keepitsecret",
		// Short classics: redundant against the length floor today, kept so
		// the list still holds if that floor ever moves.
		"password", "123456", "12345678", "qwerty", "abc123", "letmein",
		"monkey", "dragon", "iloveyou", "admin", "welcome", "login",
	} {
		commonPasswords[p] = struct{}{}
	}
}
