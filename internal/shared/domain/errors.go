package domain

import "errors"

// Domain errors shared across contexts. Adapters map these onto protocol
// error codes and CLI exit codes; the messages carry no sensitive values.
var (
	ErrVaultLocked        = errors.New("vault is locked")
	ErrVaultNotFound      = errors.New("vault does not exist")
	ErrVaultExists        = errors.New("vault already exists")
	ErrAuthenticationFail = errors.New("authentication failed")
	ErrRecordNotFound     = errors.New("record not found")
	ErrRevisionConflict   = errors.New("record revision conflict")
	ErrAuthorizationDeny  = errors.New("authorization denied")
	ErrIntegrityFailure   = errors.New("integrity failure")
	ErrValidation         = errors.New("validation failed")
	ErrClientNotFound     = errors.New("client not found")
	ErrConflict           = errors.New("operation conflicts with current state")
)
