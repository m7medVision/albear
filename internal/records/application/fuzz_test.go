package application

import "testing"

// Fuzz record payload deserialization (PRD 26.4): decode must never panic on
// arbitrary bytes and must fail cleanly rather than partially succeed.
func FuzzDecodeMetadata(f *testing.F) {
	f.Add([]byte(`{"type":"login","name":"n","urls":["https://a.example"]}`))
	f.Add([]byte(`{"urls":["::bad::"]}`))
	f.Add([]byte(`{`))
	f.Add([]byte(``))
	f.Fuzz(func(t *testing.T, data []byte) {
		_, _, _ = decodeMetadata(data)
	})
}

func FuzzDecodeSecret(f *testing.F) {
	f.Add([]byte(`{"password":"p","custom":{"a":"b"}}`))
	f.Add([]byte(`[]`))
	f.Add([]byte(`{`))
	f.Fuzz(func(t *testing.T, data []byte) {
		_, _ = decodeSecret(data)
	})
}
