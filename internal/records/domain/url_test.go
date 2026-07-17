package domain

import "testing"

func mustOrigin(t *testing.T, raw string) CanonicalOrigin {
	t.Helper()
	o, err := ParseOrigin(raw)
	if err != nil {
		t.Fatalf("ParseOrigin(%q): %v", raw, err)
	}
	return o
}

func TestParseOriginCanonicalizes(t *testing.T) {
	cases := []struct {
		raw          string
		scheme, host string
		port         string
	}{
		{"https://GitHub.com", "https", "github.com", "443"},
		{"https://github.com/login?a=b", "https", "github.com", "443"},
		{"http://example.com:8080/x", "http", "example.com", "8080"},
		{"http://example.com", "http", "example.com", "80"},
		{"https://münchen.de", "https", "xn--mnchen-3ya.de", "443"},
	}
	for _, c := range cases {
		o := mustOrigin(t, c.raw)
		if o.Scheme != c.scheme || o.Host != c.host || o.Port != c.port {
			t.Fatalf("%q → %+v, want %s://%s:%s", c.raw, o, c.scheme, c.host, c.port)
		}
	}
}

func TestParseOriginRejects(t *testing.T) {
	for _, raw := range []string{
		"", "github.com", "ftp://example.com", "javascript:alert(1)",
		"https://", "file:///etc/passwd", "https://exa mple.com",
	} {
		if _, err := ParseOrigin(raw); err == nil {
			t.Fatalf("ParseOrigin(%q) accepted", raw)
		}
	}
}

func TestOriginStringOmitsDefaultPort(t *testing.T) {
	if s := mustOrigin(t, "https://github.com").String(); s != "https://github.com" {
		t.Fatal(s)
	}
	if s := mustOrigin(t, "http://example.com:8080").String(); s != "http://example.com:8080" {
		t.Fatal(s)
	}
}

func TestOriginMatchingIsExactByDefault(t *testing.T) {
	github := mustOrigin(t, "https://github.com")

	if !github.Matches(mustOrigin(t, "https://github.com")) {
		t.Fatal("the same origin must match itself")
	}

	// A shared registrable domain is not a trust boundary: on an apex that
	// hands out subdomains, a sibling is someone else's site. Nothing but the
	// exact origin matches unless the stored URL opts in.
	for _, raw := range []string{
		"https://www.github.com",
		"https://gist.github.com",
		"https://github.com.attacker.example",
		"https://evilgithub.com",
		"https://github-login.example",
		"http://github.com",
		"https://github.com:8443",
	} {
		if github.Matches(mustOrigin(t, raw)) {
			t.Fatalf("%s must not match github.com by default", raw)
		}
	}
}

func TestOriginMatchingWithSubdomainOptIn(t *testing.T) {
	example := mustOrigin(t, "https://example.com")

	// Opting in accepts the host itself and anything under it.
	for _, raw := range []string{
		"https://example.com",
		"https://www.example.com",
		"https://accounts.example.com",
		"https://deep.nested.example.com",
	} {
		if !example.MatchesWithPolicy(mustOrigin(t, raw), true) {
			t.Fatalf("%s should match an opted-in example.com", raw)
		}
	}

	// It is an opt-in to subdomains, not to anything else. Scheme and port
	// stay mandatory, and suffix lookalikes stay out.
	for _, raw := range []string{
		"http://www.example.com",       // scheme
		"https://www.example.com:8443", // port
		"https://evil-example.com",     // not a subdomain, just a suffix
		"https://example.com.attacker.example",
		"https://notexample.com",
	} {
		if example.MatchesWithPolicy(mustOrigin(t, raw), false) {
			t.Fatalf("%s matched with the opt-in off", raw)
		}
		if example.MatchesWithPolicy(mustOrigin(t, raw), true) {
			t.Fatalf("%s must not match an opted-in example.com", raw)
		}
	}
}

// TestSubdomainOptInIsAsymmetric: the stored URL is the one the user vouched
// for. A record for the apex may cover its subdomains; a record for one
// subdomain must never reach the apex or a sibling — that is the escalation
// the exact default exists to stop.
func TestSubdomainOptInIsAsymmetric(t *testing.T) {
	apex := mustOrigin(t, "https://example.com")
	sub := mustOrigin(t, "https://accounts.example.com")
	sibling := mustOrigin(t, "https://evil.example.com")

	if !apex.MatchesWithPolicy(sub, true) {
		t.Fatal("opted-in apex should cover its subdomain")
	}
	if sub.MatchesWithPolicy(apex, true) {
		t.Fatal("a subdomain record reached the apex")
	}
	if sub.MatchesWithPolicy(sibling, true) {
		t.Fatal("a subdomain record reached a sibling")
	}
}

// TestLoginURLCarriesItsOwnPolicy: policy is per URL, so one record can hold
// an exact URL alongside an opted-in one.
func TestLoginURLCarriesItsOwnPolicy(t *testing.T) {
	exact, err := NewLoginURL("https://github.com")
	if err != nil {
		t.Fatal(err)
	}
	if exact.AllowSubdomains {
		t.Fatal("NewLoginURL opted into subdomains")
	}
	if exact.Matches(mustOrigin(t, "https://www.github.com")) {
		t.Fatal("a plain login URL matched a subdomain")
	}

	loose, err := NewLoginURLWithPolicy("https://github.com", true)
	if err != nil {
		t.Fatal(err)
	}
	if !loose.Matches(mustOrigin(t, "https://www.github.com")) {
		t.Fatal("an opted-in login URL did not match a subdomain")
	}
	if loose.Matches(mustOrigin(t, "https://evilgithub.com")) {
		t.Fatal("an opted-in login URL matched a lookalike")
	}
}

func TestIsSecure(t *testing.T) {
	if !mustOrigin(t, "https://a.example").IsSecure() {
		t.Fatal("https not secure")
	}
	if mustOrigin(t, "http://a.example").IsSecure() {
		t.Fatal("http reported secure")
	}
}

func TestNewLoginURL(t *testing.T) {
	u, err := NewLoginURL("https://github.com/login")
	if err != nil {
		t.Fatal(err)
	}
	if u.Origin.Host != "github.com" {
		t.Fatal(u.Origin.Host)
	}
	if _, err := NewLoginURL("not a url at all ::"); err == nil {
		t.Fatal("invalid URL accepted")
	}
}

// TestParseOriginRejectsHostsThatMapToNothing: IDNA maps some code points to
// nothing, so a host that is non-empty on the way in can be empty on the way
// out. Found by FuzzParseOrigin. An origin with no host is not an origin, and
// two of them would compare equal to each other.
func TestParseOriginRejectsHostsThatMapToNothing(t *testing.T) {
	for _, raw := range []string{
		"http://­",   // soft hyphen, alone
		"https://­",  //
		"https://​",  // zero-width space
		"https://­­", //
	} {
		if o, err := ParseOrigin(raw); err == nil {
			t.Fatalf("ParseOrigin(%q) accepted, giving host %q", raw, o.Host)
		}
	}
}
