package client

import (
	"encoding/hex"
	"strings"
	"testing"

	transport "albear/internal/infrastructure/transport/noise"
)

func TestIdentityRoundTrip(t *testing.T) {
	key, err := transport.GenerateStaticKey()
	if err != nil {
		t.Fatal(err)
	}
	id := Identity{
		ClientID:        "aabb",
		Credential:      hex.EncodeToString(make([]byte, 32)),
		DaemonStaticKey: hex.EncodeToString(key.Public),
		StaticPublic:    hex.EncodeToString(key.Public),
		StaticPrivate:   hex.EncodeToString(key.Private),
	}
	cred, err := id.CredentialBytes()
	if err != nil || len(cred) != 32 {
		t.Fatal(err)
	}
	dk, err := id.DaemonKeyBytes()
	if err != nil || len(dk) != 32 {
		t.Fatal(err)
	}
	sk, err := id.StaticKey()
	if err != nil || len(sk.Public) != 32 || len(sk.Private) != 32 {
		t.Fatal(err)
	}

	bad := Identity{StaticPublic: "zz", StaticPrivate: "zz"}
	if _, err := bad.StaticKey(); err == nil {
		t.Fatal("bad hex accepted")
	}
	bad2 := Identity{StaticPublic: hex.EncodeToString(key.Public), StaticPrivate: "zz"}
	if _, err := bad2.StaticKey(); err == nil {
		t.Fatal("bad private hex accepted")
	}
}

func TestDialUnavailable(t *testing.T) {
	if _, err := DialCLI("/nonexistent/albear.sock"); err == nil {
		t.Fatal("dial to missing socket succeeded")
	} else if !strings.Contains(err.Error(), "daemon unavailable") {
		t.Fatal(err)
	}
}

func TestAPIErrorFormat(t *testing.T) {
	e := &APIError{Code: "VAULT_LOCKED", Message: "The vault is locked."}
	if e.Error() != "VAULT_LOCKED: The vault is locked." {
		t.Fatal(e.Error())
	}
}
