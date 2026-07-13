package domain

import (
	"encoding/json"
	"fmt"
	"strings"
	"testing"
)

func TestIDRoundTrip(t *testing.T) {
	raw := []byte("0123456789abcdef")
	id, err := IDFromBytes(raw)
	if err != nil {
		t.Fatal(err)
	}
	back, err := IDFromString(id.String())
	if err != nil {
		t.Fatal(err)
	}
	if back != id {
		t.Fatal("string round trip mismatch")
	}
	if string(id.Bytes()) != string(raw) {
		t.Fatal("bytes mismatch")
	}
}

func TestIDValidation(t *testing.T) {
	if _, err := IDFromBytes([]byte("short")); err != ErrInvalidID {
		t.Fatal("short id accepted")
	}
	if _, err := IDFromString("zz"); err != ErrInvalidID {
		t.Fatal("bad hex accepted")
	}
	var zero ID
	if !zero.IsZero() {
		t.Fatal("zero id not zero")
	}
}

func TestSecretStringNeverLeaks(t *testing.T) {
	s := NewSecretFromString("super-secret-password")

	// fmt verbs must not print the raw value.
	for _, out := range []string{
		fmt.Sprintf("%v", s), fmt.Sprintf("%+v", s), fmt.Sprintf("%#v", s), fmt.Sprint(s),
	} {
		if strings.Contains(out, "super-secret-password") {
			t.Fatalf("secret leaked through fmt: %q", out)
		}
	}

	// JSON marshaling must emit the redaction placeholder.
	b, err := json.Marshal(struct{ P SecretString }{s})
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(string(b), "super-secret") || !strings.Contains(string(b), "[redacted]") {
		t.Fatalf("secret leaked through JSON: %s", b)
	}
}

func TestSecretStringExposeAndWipe(t *testing.T) {
	s := NewSecret([]byte("value"))
	if string(s.Expose()) != "value" || s.Len() != 5 || s.IsEmpty() {
		t.Fatal("expose broken")
	}
	s.Wipe()
	for _, b := range s.Expose() {
		if b != 0 {
			t.Fatal("wipe incomplete")
		}
	}
	if !NewSecret(nil).IsEmpty() {
		t.Fatal("empty secret not empty")
	}
}

func TestNewSecretCopies(t *testing.T) {
	src := []byte("abc")
	s := NewSecret(src)
	src[0] = 'x'
	if string(s.Expose()) != "abc" {
		t.Fatal("secret aliases caller buffer")
	}
}
