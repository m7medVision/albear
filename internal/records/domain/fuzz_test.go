package domain

import "testing"

// FuzzParseOrigin: URL normalization must never panic and never produce a
// host-less origin (PRD 26.4).
func FuzzParseOrigin(f *testing.F) {
	for _, seed := range []string{
		"https://github.com", "http://a.b:8080/x?y=z", "https://münchen.de",
		"javascript:alert(1)", "//evil", "https://github.com.attacker.example",
		"https://[::1]:443", "http://%zz", "",
	} {
		f.Add(seed)
	}
	f.Fuzz(func(t *testing.T, raw string) {
		o, err := ParseOrigin(raw)
		if err != nil {
			return
		}
		if o.Host == "" || (o.Scheme != "http" && o.Scheme != "https") || o.Port == "" {
			t.Fatalf("accepted origin with missing parts: %+v from %q", o, raw)
		}
		// Matching against itself must hold and never panic.
		if !o.Matches(o) {
			t.Fatalf("origin does not match itself: %+v", o)
		}
	})
}
