package protocol

import (
	"errors"
	"strings"
	"testing"

	shared "github.com/m7medVision/albear/internal/shared/domain"
	vaultapp "github.com/m7medVision/albear/internal/vault/application"
)

func TestMapErrorCodes(t *testing.T) {
	cases := map[string]error{
		CodeVaultLocked:   shared.ErrVaultLocked,
		CodeAuthFailed:    shared.ErrAuthenticationFail,
		CodeNotFound:      shared.ErrRecordNotFound,
		CodeDenied:        shared.ErrAuthorizationDeny,
		CodeIntegrity:     shared.ErrIntegrityFailure,
		CodeConflict:      shared.ErrRevisionConflict,
		CodeInvalid:       shared.ErrValidation,
		CodeExists:        shared.ErrVaultExists,
		CodeUninitialized: shared.ErrVaultNotFound,
		CodeRateLimited:   vaultapp.ErrRateLimited,
		CodeInternal:      errors.New("some internal detail"),
	}
	for code, err := range cases {
		we := MapError(err)
		if we.Code != code {
			t.Fatalf("%v → %s, want %s", err, we.Code, code)
		}
		if we.Message == "" {
			t.Fatalf("%s has no message", code)
		}
	}
	// Internal errors never leak their text.
	if we := MapError(errors.New("sql: table records has 7 rows")); strings.Contains(we.Message, "sql") {
		t.Fatal("internal error text leaked")
	}
}

func TestEnvelopes(t *testing.T) {
	resp, err := OKResponse("r1", map[string]int{"n": 1})
	if err != nil || !resp.OK || resp.RequestID != "r1" || resp.ProtocolVersion != Version {
		t.Fatalf("%+v %v", resp, err)
	}
	if string(resp.Data) != `{"n":1}` {
		t.Fatal(string(resp.Data))
	}

	er := ErrResponse("r2", shared.ErrVaultLocked)
	if er.OK || er.Error.Code != CodeVaultLocked || er.RequestID != "r2" {
		t.Fatalf("%+v", er)
	}

	// Unmarshalable data fails cleanly.
	if _, err := OKResponse("r3", make(chan int)); err == nil {
		t.Fatal("channel marshaled")
	}
}
