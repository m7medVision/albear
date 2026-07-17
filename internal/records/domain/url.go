package domain

import (
	"net/url"
	"strings"

	"golang.org/x/net/idna"
	"golang.org/x/net/publicsuffix"

	shared "github.com/m7medVision/albear/internal/shared/domain"
)

// CanonicalOrigin is a parsed, normalized web origin: scheme, punycoded
// lowercase host, and effective port. Matching operates on these values,
// never on substrings (PRD section 13.3).
type CanonicalOrigin struct {
	Scheme string
	Host   string
	Port   string
}

// ParseOrigin canonicalizes an origin or URL string. Anything without an
// http/https scheme and a host is rejected.
func ParseOrigin(raw string) (CanonicalOrigin, error) {
	u, err := url.Parse(strings.TrimSpace(raw))
	if err != nil {
		return CanonicalOrigin{}, shared.ErrValidation
	}
	scheme := strings.ToLower(u.Scheme)
	if scheme != "http" && scheme != "https" {
		return CanonicalOrigin{}, shared.ErrValidation
	}
	host := strings.ToLower(u.Hostname())
	if host == "" {
		return CanonicalOrigin{}, shared.ErrValidation
	}
	// Internationalized domain normalization: everything is compared in
	// punycode ASCII form so Unicode lookalikes cannot alias an ASCII host.
	ascii, err := idna.Lookup.ToASCII(host)
	if err != nil {
		return CanonicalOrigin{}, shared.ErrValidation
	}
	// Re-check emptiness after mapping, not just before it. IDNA maps some
	// code points to nothing at all — "http://­" (a lone soft hyphen) has
	// a non-empty host going in and an empty one coming out — and an origin
	// with no host would compare equal to any other host-less origin.
	if ascii == "" {
		return CanonicalOrigin{}, shared.ErrValidation
	}
	port := u.Port()
	if port == "" {
		if scheme == "https" {
			port = "443"
		} else {
			port = "80"
		}
	}
	return CanonicalOrigin{Scheme: scheme, Host: ascii, Port: port}, nil
}

func (o CanonicalOrigin) String() string {
	defaultPort := (o.Scheme == "https" && o.Port == "443") || (o.Scheme == "http" && o.Port == "80")
	if defaultPort {
		return o.Scheme + "://" + o.Host
	}
	return o.Scheme + "://" + o.Host + ":" + o.Port
}

// IsSecure reports whether the origin uses https. HTTP filling is disabled by
// default and requires an explicit per-site override at a higher layer.
func (o CanonicalOrigin) IsSecure() bool { return o.Scheme == "https" }

// RegistrableDomain returns the eTLD+1 of the host ("www.github.com" →
// "github.com"). IP addresses and single-label hosts return the host itself.
func (o CanonicalOrigin) RegistrableDomain() string {
	d, err := publicsuffix.EffectiveTLDPlusOne(o.Host)
	if err != nil {
		return o.Host
	}
	return d
}

// Matches is exact equality of scheme, punycode host, and effective port.
//
// This is the default because a shared registrable domain is not a trust
// boundary. On an apex that hands subdomains to strangers — github.io,
// the big cloud app domains, most universities — "shares an eTLD+1" means
// "someone else's site", and matching there would offer a credential for
// accounts.example.com to evil.example.com. A record opts into the looser
// rule per URL; see MatchesWithPolicy.
func (o CanonicalOrigin) Matches(other CanonicalOrigin) bool {
	return o.Scheme == other.Scheme && o.Host == other.Host && o.Port == other.Port
}

// MatchesWithPolicy is Matches, plus at-or-under-host matching when the stored
// URL opted in. Scheme and effective port stay mandatory either way: opting
// into subdomains is not opting into http, or into a different port.
//
// The relation is deliberately asymmetric. The receiver is the *stored* URL —
// the one the user vouched for — and other is the page asking. A record for
// example.com with the flag set matches www.example.com, but a record for
// www.example.com never matches example.com, let alone a sibling.
func (o CanonicalOrigin) MatchesWithPolicy(other CanonicalOrigin, allowSubdomains bool) bool {
	if !allowSubdomains {
		return o.Matches(other)
	}
	if o.Scheme != other.Scheme || o.Port != other.Port {
		return false
	}
	// isUnder is suffix matching against a dot-delimited boundary, so
	// "evil-example.com" and "example.com.attacker.example" do not qualify.
	return isUnder(other.Host, o.Host)
}

func isUnder(host, apex string) bool {
	return host == apex || strings.HasSuffix(host, "."+apex)
}

// LoginURL is a validated URL attached to a login record.
type LoginURL struct {
	Raw    string
	Origin CanonicalOrigin
	// AllowSubdomains widens matching for this URL to hosts at or under
	// Origin.Host. It is off unless the user turned it on, and it lives in the
	// encrypted metadata blob rather than a plaintext column, so the set of
	// sites with relaxed matching is not readable from the database file.
	AllowSubdomains bool
}

func NewLoginURL(raw string) (LoginURL, error) {
	o, err := ParseOrigin(raw)
	if err != nil {
		return LoginURL{}, err
	}
	return LoginURL{Raw: raw, Origin: o}, nil
}

// NewLoginURLWithPolicy builds a URL that also matches hosts under its own.
func NewLoginURLWithPolicy(raw string, allowSubdomains bool) (LoginURL, error) {
	u, err := NewLoginURL(raw)
	if err != nil {
		return LoginURL{}, err
	}
	u.AllowSubdomains = allowSubdomains
	return u, nil
}

// Matches reports whether the page origin is covered by this stored URL under
// its own policy.
func (u LoginURL) Matches(page CanonicalOrigin) bool {
	return u.Origin.MatchesWithPolicy(page, u.AllowSubdomains)
}
