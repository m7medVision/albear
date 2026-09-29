package crx

import (
	"archive/zip"
	"bytes"
	"crypto/rand"
	"crypto/rsa"
	"encoding/base64"
	"encoding/binary"
	"os"
	"path/filepath"
	"testing"
)

func TestExtensionID(t *testing.T) {
	// The pinned prod key and the ID Chrome shows for it.
	der, err := base64.StdEncoding.DecodeString("MIIBIjANBgkqhkiG9w0BAQEFAAOCAQ8AMIIBCgKCAQEA0rUlbOwcLAW75Zoj9wSrnZM1edjGIIRFcuBzwvpSRD5KjI0I72IJrf4rcnamNOXcJRqILk7gd361HOAPnb6NrlyIWqMSwbm1G9xgDs1ew8HOMJ/8xMwzZxq4jDSVtZgWKn+p7ebWSmcrEcttIBaow2YS+RwnFQjbO7u4DxtZxkyYwAIHeEoveK0ekuH1TApdxdJu8Jz1JMmXIgjHVH+/5p6FaZgEND6UuoDOGhC8XTH2r8ioK9zvvRb8QH98qn8c9VvsoX23RoKxyjs0vkMQFsLeWjxPK6EV6fY9VFGg3vbusyIRa5F9X66InQfCMu89JB8dTfOFoVqbAAxWXnMkVwIDAQAB")
	if err != nil {
		t.Fatal(err)
	}
	if got, want := ExtensionID(der), "legbdpcjojmfelbcjfelmdelnjcnpllc"; got != want {
		t.Fatalf("ExtensionID = %s, want %s", got, want)
	}
}

func testKey(t *testing.T) *rsa.PrivateKey {
	t.Helper()
	key, err := rsa.GenerateKey(rand.Reader, 2048)
	if err != nil {
		t.Fatal(err)
	}
	return key
}

func testArchive(t *testing.T, version string) []byte {
	t.Helper()
	dir := t.TempDir()
	files := map[string]string{
		"manifest.json":        `{"manifest_version":3,"name":"t","version":"` + version + `","key":"k"}`,
		"assets/background.js": "console.log(1)",
	}
	for name, body := range files {
		p := filepath.Join(dir, name)
		if err := os.MkdirAll(filepath.Dir(p), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(p, []byte(body), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	archive, err := ZipDir(dir)
	if err != nil {
		t.Fatal(err)
	}
	return archive
}

func TestPackVerifyRoundTrip(t *testing.T) {
	key := testKey(t)
	archive := testArchive(t, "1.4.2")
	data, err := Pack(archive, key)
	if err != nil {
		t.Fatal(err)
	}
	if string(data[:4]) != "Cr24" || binary.LittleEndian.Uint32(data[4:8]) != 3 {
		t.Fatalf("bad preamble % x", data[:8])
	}
	if !bytes.HasSuffix(data, archive) {
		t.Fatal("archive is not the file's tail")
	}

	pkg, err := Verify(data)
	if err != nil {
		t.Fatal(err)
	}
	pub, err := PublicKeyDER(key)
	if err != nil {
		t.Fatal(err)
	}
	if pkg.ID != ExtensionID(pub) || !bytes.Equal(pkg.PublicKey, pub) || !bytes.Equal(pkg.Archive, archive) {
		t.Fatalf("Verify = {ID:%s}, want ID %s and the packed archive", pkg.ID, ExtensionID(pub))
	}
	m, err := ReadManifest(pkg.Archive)
	if err != nil {
		t.Fatal(err)
	}
	if m.Version != "1.4.2" || m.Key != "k" {
		t.Fatalf("manifest = %+v", m)
	}
	zr, err := zip.NewReader(bytes.NewReader(pkg.Archive), int64(len(pkg.Archive)))
	if err != nil {
		t.Fatal(err)
	}
	if _, err := zr.Open("assets/background.js"); err != nil {
		t.Fatalf("nested file missing: %v", err)
	}
}

func TestVerifyRejects(t *testing.T) {
	key := testKey(t)
	good, err := Pack(testArchive(t, "1.0.0"), key)
	if err != nil {
		t.Fatal(err)
	}
	headerEnd := 12 + int(binary.LittleEndian.Uint32(good[8:12]))

	// A proof from a key other than the one crx_id names.
	other, err := Pack(testArchive(t, "1.0.0"), testKey(t))
	if err != nil {
		t.Fatal(err)
	}

	mutate := func(f func(b []byte) []byte) []byte {
		return f(append([]byte(nil), good...))
	}
	tests := []struct {
		name string
		data []byte
	}{
		{"empty", nil},
		{"bad magic", mutate(func(b []byte) []byte { b[0] = 'X'; return b })},
		{"crx2", mutate(func(b []byte) []byte { b[4] = 2; return b })},
		{"header too large", mutate(func(b []byte) []byte { binary.LittleEndian.PutUint32(b[8:12], 1<<30); return b })},
		{"tampered archive", mutate(func(b []byte) []byte { b[len(b)-30] ^= 1; return b })},
		{"appended data", mutate(func(b []byte) []byte { return append(b, 0) })},
		{"tampered header", mutate(func(b []byte) []byte { b[headerEnd-3] ^= 1; return b })},
		{"swapped crx_id", swapSignedHeader(t, good, other)},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if _, err := Verify(tt.data); err == nil {
				t.Fatal("Verify succeeded")
			}
		})
	}
}

// swapSignedHeader re-signs nothing: it takes a's proof and b's crx_id, which
// must fail because the signature covers signed_header_data.
func swapSignedHeader(t *testing.T, a, b []byte) []byte {
	t.Helper()
	hdr := func(d []byte) []byte { return d[12 : 12+binary.LittleEndian.Uint32(d[8:12])] }
	var proof, signed []byte
	if err := walkFields(hdr(a), func(f uint64, v []byte) {
		if f == fieldSHA256WithRSA {
			proof = v
		}
	}); err != nil {
		t.Fatal(err)
	}
	if err := walkFields(hdr(b), func(f uint64, v []byte) {
		if f == fieldSignedHeaderData {
			signed = v
		}
	}); err != nil {
		t.Fatal(err)
	}
	h := appendBytesField(nil, fieldSHA256WithRSA, proof)
	h = appendBytesField(h, fieldSignedHeaderData, signed)
	out := append([]byte("Cr24"), 3, 0, 0, 0)
	out = binary.LittleEndian.AppendUint32(out, uint32(len(h)))
	out = append(out, h...)
	return append(out, a[12+len(hdr(a)):]...)
}

func TestParsePrivateKey(t *testing.T) {
	if _, err := ParsePrivateKey([]byte("not a key")); err == nil {
		t.Fatal("ParsePrivateKey accepted garbage")
	}
	// The committed dev key parses, with its explanatory text around it.
	data, err := os.ReadFile(filepath.Join("..", "..", "extension", "keys", "dev.pem"))
	if err != nil {
		t.Fatal(err)
	}
	if _, err := ParsePrivateKey(data); err != nil {
		t.Fatal(err)
	}
}
