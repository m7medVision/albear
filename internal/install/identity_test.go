package install

import (
	"crypto"
	"crypto/sha256"
	"crypto/x509"
	"encoding/base64"
	"encoding/hex"
	"encoding/pem"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"testing"

	"github.com/m7medVision/albear/internal/version"
)

func TestIdentityFor(t *testing.T) {
	tests := []struct {
		env  version.Environment
		want Identity
	}{
		{version.Prod, Identity{HostName: "dev.albear.native", ExtensionID: ChromeExtensionID}},
		{version.Dev, Identity{HostName: "dev.albear.native_dev", ExtensionID: DevExtensionID}},
	}
	for _, tt := range tests {
		if got := IdentityFor(tt.env); got != tt.want {
			t.Errorf("IdentityFor(%s) = %+v, want %+v", tt.env, got, tt.want)
		}
	}
}

// extensionID is Chrome's ID for a DER public key: the first 16 bytes of its
// SHA-256, hex digits 0-f mapped to a-p.
func extensionID(der []byte) string {
	sum := sha256.Sum256(der)
	return strings.Map(func(r rune) rune {
		if r >= 'a' {
			return 'k' + (r - 'a')
		}
		return 'a' + (r - '0')
	}, hex.EncodeToString(sum[:16]))
}

// The IDs here must be the ones the extension build pins, or the native host
// would allow an extension that never connects.
func TestExtensionIDsMatchKeys(t *testing.T) {
	root := filepath.Join("..", "..")
	pemData, err := os.ReadFile(filepath.Join(root, "extension", "keys", "dev.pem"))
	if err != nil {
		t.Fatal(err)
	}
	block, _ := pem.Decode(pemData)
	if block == nil {
		t.Fatal("extension/keys/dev.pem has no PEM block")
	}
	key, err := x509.ParsePKCS8PrivateKey(block.Bytes)
	if err != nil {
		t.Fatal(err)
	}
	pub, err := x509.MarshalPKIXPublicKey(key.(crypto.Signer).Public())
	if err != nil {
		t.Fatal(err)
	}
	if got := extensionID(pub); got != DevExtensionID {
		t.Errorf("extension/keys/dev.pem is extension %s, DevExtensionID is %s", got, DevExtensionID)
	}

	src, err := os.ReadFile(filepath.Join(root, "extension", "build", "target.ts"))
	if err != nil {
		t.Fatal(err)
	}
	for constant, want := range map[string]string{
		"PROD_PUBLIC_KEY": ChromeExtensionID,
		"DEV_PUBLIC_KEY":  DevExtensionID,
	} {
		m := regexp.MustCompile(constant + `\s*=\s*'([^']+)'`).FindSubmatch(src)
		if m == nil {
			t.Fatalf("%s not found in extension/build/target.ts", constant)
		}
		der, err := base64.StdEncoding.DecodeString(string(m[1]))
		if err != nil {
			t.Fatal(err)
		}
		if got := extensionID(der); got != want {
			t.Errorf("%s pins extension %s, want %s", constant, got, want)
		}
	}
}
