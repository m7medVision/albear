// Package domain enumerates security event codes and severities. Codes are
// numeric so locked-state events carry no sensitive content (PRD 17.3).
package domain

type Severity int

const (
	SeverityInfo Severity = iota + 1
	SeverityWarning
	SeverityCritical
)

type EventCode int

const (
	EventVaultCreated EventCode = iota + 100
	EventVaultUnlocked
	EventVaultLocked
	EventVaultPanicLocked
	EventMasterPasswordChanged
	EventUnlockFailed
	EventUnlockRateLimited
	EventClientPairingRequested
	EventClientApproved
	EventClientRevoked
	EventClientAutoApproved
	EventUnauthorizedRequest
	EventIntegrityFailure
	EventTransportHandshakeFailed
	EventProtocolViolation
	EventBackupCreated
	EventBackupRestored
	EventVaultDestroyed
	// Append only. These are iota-derived and stored as integers in
	// security_events, so inserting above would silently relabel history.
	EventVaultStateBootstrapped
)
