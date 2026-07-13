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

func TestOriginMatching(t *testing.T) {
	github := mustOrigin(t, "https://github.com")

	// Valid matches (PRD 13.3).
	for _, raw := range []string{"https://github.com", "https://www.github.com", "https://gist.github.com"} {
		if !github.Matches(mustOrigin(t, raw)) {
			t.Fatalf("%s should match github.com", raw)
		}
	}

	// Invalid matches: lookalikes, wrong scheme, wrong port.
	for _, raw := range []string{
		"https://github.com.attacker.example",
		"https://evilgithub.com",
		"https://github-login.example",
		"http://github.com",
		"https://github.com:8443",
	} {
		if github.Matches(mustOrigin(t, raw)) {
			t.Fatalf("%s must not match github.com", raw)
		}
	}
}

func TestOriginMatchingIsSymmetric(t *testing.T) {
	a := mustOrigin(t, "https://www.github.com")
	b := mustOrigin(t, "https://github.com")
	if !a.Matches(b) || !b.Matches(a) {
		t.Fatal("matching must be symmetric")
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
