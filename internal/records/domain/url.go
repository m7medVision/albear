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

// Matches implements the subdomain policy: two origins match when their
// schemes and effective ports are equal and they share a registrable domain.
// This accepts github.com ↔ www.github.com and rejects lookalikes such as
// github.com.attacker.example (registrable domain attacker.example).
func (o CanonicalOrigin) Matches(other CanonicalOrigin) bool {
	if o.Scheme != other.Scheme || o.Port != other.Port {
		return false
	}
	if o.Host == other.Host {
		return true
	}
	ra, rb := o.RegistrableDomain(), other.RegistrableDomain()
	if ra == "" || rb == "" || ra != rb {
		return false
	}
	// Both hosts must actually sit under the shared registrable domain.
	return isUnder(o.Host, ra) && isUnder(other.Host, rb)
}

func isUnder(host, apex string) bool {
	return host == apex || strings.HasSuffix(host, "."+apex)
}

// LoginURL is a validated URL attached to a login record.
type LoginURL struct {
	Raw    string
	Origin CanonicalOrigin
}

func NewLoginURL(raw string) (LoginURL, error) {
	o, err := ParseOrigin(raw)
	if err != nil {
		return LoginURL{}, err
	}
	return LoginURL{Raw: raw, Origin: o}, nil
}
