// Package protocol defines the JSON request/response envelopes exchanged
// inside Noise payloads (PRD 24) and the mapping from domain errors to wire
// codes. Internal error text never crosses the daemon boundary.
package protocol

import (
	"encoding/json"
	"errors"

	shared "github.com/m7medVision/albear/internal/shared/domain"
	vaultapp "github.com/m7medVision/albear/internal/vault/application"
)

const Version = 1

type Request struct {
	ProtocolVersion int             `json:"protocolVersion"`
	RequestID       string          `json:"requestId"`
	Operation       string          `json:"operation"`
	Payload         json.RawMessage `json:"payload,omitempty"`
}

type Response struct {
	ProtocolVersion int             `json:"protocolVersion"`
	RequestID       string          `json:"requestId"`
	OK              bool            `json:"ok"`
	Data            json.RawMessage `json:"data,omitempty"`
	Error           *WireError      `json:"error,omitempty"`
}

type WireError struct {
	Code    string `json:"code"`
	Message string `json:"message"`
}

// Error codes (PRD 14.4 maps these to CLI exit codes).
const (
	CodeVaultLocked   = "VAULT_LOCKED"
	CodeAuthFailed    = "AUTH_FAILED"
	CodeNotFound      = "NOT_FOUND"
	CodeDenied        = "DENIED"
	CodeIntegrity     = "INTEGRITY_FAILURE"
	CodeConflict      = "CONFLICT"
	CodeInvalid       = "INVALID_REQUEST"
	CodeRateLimited   = "RATE_LIMITED"
	CodeExists        = "ALREADY_EXISTS"
	CodeUninitialized = "NOT_INITIALIZED"
	CodeInternal      = "INTERNAL"
)

var codeMessages = map[string]string{
	CodeVaultLocked:   "The vault is locked.",
	CodeAuthFailed:    "Authentication failed.",
	CodeNotFound:      "Record not found.",
	CodeDenied:        "Authorization denied.",
	CodeIntegrity:     "Integrity failure detected.",
	CodeConflict:      "Revision conflict.",
	CodeInvalid:       "Invalid request.",
	CodeRateLimited:   "Too many attempts. Try again later.",
	CodeExists:        "The vault already exists.",
	CodeUninitialized: "No vault exists. Run `vault init` first.",
	CodeInternal:      "Internal failure.",
}

// MapError converts any error into a wire error with a fixed, non-sensitive
// message.
func MapError(err error) *WireError {
	code := CodeInternal
	switch {
	case errors.Is(err, shared.ErrVaultLocked):
		code = CodeVaultLocked
	case errors.Is(err, shared.ErrAuthenticationFail):
		code = CodeAuthFailed
	case errors.Is(err, shared.ErrRecordNotFound), errors.Is(err, shared.ErrClientNotFound):
		code = CodeNotFound
	case errors.Is(err, shared.ErrAuthorizationDeny):
		code = CodeDenied
	case errors.Is(err, shared.ErrIntegrityFailure):
		code = CodeIntegrity
	case errors.Is(err, shared.ErrRevisionConflict):
		code = CodeConflict
	case errors.Is(err, shared.ErrValidation):
		code = CodeInvalid
	case errors.Is(err, shared.ErrVaultExists):
		code = CodeExists
	case errors.Is(err, shared.ErrVaultNotFound):
		code = CodeUninitialized
	case errors.Is(err, vaultapp.ErrRateLimited):
		code = CodeRateLimited
	}
	return &WireError{Code: code, Message: codeMessages[code]}
}

// OKResponse builds a success envelope; data must marshal cleanly.
func OKResponse(requestID string, data any) (*Response, error) {
	raw, err := json.Marshal(data)
	if err != nil {
		return nil, err
	}
	return &Response{ProtocolVersion: Version, RequestID: requestID, OK: true, Data: raw}, nil
}

// ErrResponse builds a failure envelope from any error.
func ErrResponse(requestID string, err error) *Response {
	return &Response{ProtocolVersion: Version, RequestID: requestID, OK: false, Error: MapError(err)}
}
