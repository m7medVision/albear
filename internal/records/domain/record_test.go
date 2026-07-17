package domain

import (
	"testing"

	shared "github.com/m7medVision/albear/internal/shared/domain"
)

func testID(t *testing.T) shared.ID {
	t.Helper()
	id, err := shared.IDFromBytes([]byte("0123456789abcdef"))
	if err != nil {
		t.Fatal(err)
	}
	return id
}

func validLogin(t *testing.T) *Record {
	u, _ := NewLoginURL("https://github.com")
	return &Record{
		ID:       testID(t),
		Type:     TypeLogin,
		Revision: 1,
		Metadata: RecordMetadata{Name: "GitHub", Username: "mo", URLs: []LoginURL{u}},
		Secret:   SecretPayload{Password: shared.NewSecretFromString("hunter2")},
	}
}

func TestValidateLogin(t *testing.T) {
	if err := validLogin(t).Validate(); err != nil {
		t.Fatal(err)
	}
}

func TestValidateRejects(t *testing.T) {
	cases := map[string]func(*Record){
		"zero id":       func(r *Record) { r.ID = shared.ID{} },
		"bad type":      func(r *Record) { r.Type = "bogus" },
		"zero revision": func(r *Record) { r.Revision = 0 },
		"empty name":    func(r *Record) { r.Metadata.Name = "" },
		"empty login": func(r *Record) {
			r.Metadata.Username = ""
			r.Metadata.URLs = nil
			r.Secret = SecretPayload{}
		},
		"invalid url": func(r *Record) { r.Metadata.URLs = []LoginURL{{Raw: "x"}} },
	}
	for name, mutate := range cases {
		r := validLogin(t)
		mutate(r)
		if err := r.Validate(); err == nil {
			t.Fatalf("%s: validation passed", name)
		}
	}
}

func TestValidateAPICredential(t *testing.T) {
	r := &Record{
		ID:       testID(t),
		Type:     TypeAPICredential,
		Revision: 1,
		Metadata: RecordMetadata{Name: "Stripe", Service: "stripe"},
	}
	if err := r.Validate(); err == nil {
		t.Fatal("api credential without key accepted")
	}
	r.Secret.APIKey = shared.NewSecretFromString("sk_live_x")
	if err := r.Validate(); err != nil {
		t.Fatal(err)
	}
}

func TestValidateNote(t *testing.T) {
	r := &Record{
		ID:       testID(t),
		Type:     TypeSecureNote,
		Revision: 1,
		Metadata: RecordMetadata{Name: "recovery codes"},
		Secret:   SecretPayload{Notes: shared.NewSecretFromString("codes...")},
	}
	if err := r.Validate(); err != nil {
		t.Fatal(err)
	}
}

func TestMatchesOrigin(t *testing.T) {
	r := validLogin(t)
	exact, _ := ParseOrigin("https://github.com/login")
	if !r.MatchesOrigin(exact) {
		t.Fatal("the record's own origin should match")
	}
	// Exact by default: a subdomain is a different site until the record says
	// otherwise.
	sub, _ := ParseOrigin("https://www.github.com/login")
	if r.MatchesOrigin(sub) {
		t.Fatal("www subdomain matched without the opt-in")
	}
	evil, _ := ParseOrigin("https://github.com.attacker.example")
	if r.MatchesOrigin(evil) {
		t.Fatal("lookalike matched")
	}
}

// TestMatchesOriginPerURLPolicy: policy is a property of each stored URL, so a
// record can be exact about one site and permissive about another.
func TestMatchesOriginPerURLPolicy(t *testing.T) {
	strict, err := NewLoginURL("https://github.com")
	if err != nil {
		t.Fatal(err)
	}
	loose, err := NewLoginURLWithPolicy("https://example.com", true)
	if err != nil {
		t.Fatal(err)
	}
	r := validLogin(t)
	r.Metadata.URLs = []LoginURL{strict, loose}

	sub, _ := ParseOrigin("https://www.example.com")
	if !r.MatchesOrigin(sub) {
		t.Fatal("the opted-in URL did not match its subdomain")
	}
	ghSub, _ := ParseOrigin("https://www.github.com")
	if r.MatchesOrigin(ghSub) {
		t.Fatal("the opt-in on one URL leaked to another")
	}
}

func TestSecretPayloadWipe(t *testing.T) {
	p := SecretPayload{
		Password:     shared.NewSecretFromString("pw"),
		CustomValues: map[string]shared.SecretString{"k": shared.NewSecretFromString("v")},
	}
	p.Wipe()
	for _, b := range p.Password.Expose() {
		if b != 0 {
			t.Fatal("password not wiped")
		}
	}
	for _, b := range p.CustomValues["k"].Expose() {
		if b != 0 {
			t.Fatal("custom value not wiped")
		}
	}
}
