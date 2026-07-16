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
	page, _ := ParseOrigin("https://www.github.com/login")
	if !r.MatchesOrigin(page) {
		t.Fatal("www subdomain should match")
	}
	evil, _ := ParseOrigin("https://github.com.attacker.example")
	if r.MatchesOrigin(evil) {
		t.Fatal("lookalike matched")
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
