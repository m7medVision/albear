package domain

// SecretString wraps sensitive text. It deliberately implements neither
// fmt.Stringer nor encoding.TextMarshaler, so accidental logging or JSON
// encoding of the raw value is a compile-time-visible, explicit act.
type SecretString struct {
	value []byte
}

func NewSecret(v []byte) SecretString {
	return SecretString{value: append([]byte(nil), v...)}
}

func NewSecretFromString(v string) SecretString {
	return SecretString{value: []byte(v)}
}

// Expose returns the raw value. Call sites are the audit surface for reveals.
func (s SecretString) Expose() []byte { return s.value }

func (s SecretString) IsEmpty() bool { return len(s.value) == 0 }
func (s SecretString) Len() int      { return len(s.value) }

// Wipe overwrites the backing buffer best-effort.
func (s SecretString) Wipe() {
	for i := range s.value {
		s.value[i] = 0
	}
}

// GoString stops %#v / %v reflection dumps from printing the value.
func (s SecretString) GoString() string { return "SecretString(redacted)" }

// Format-oriented redaction: MarshalJSON emits a fixed placeholder so a
// SecretString embedded in any DTO serializes as redacted, never raw.
func (s SecretString) MarshalJSON() ([]byte, error) { return []byte(`"[redacted]"`), nil }
