package domain

// Capability is a bit in a client's capability mask. Authorization is a pure
// mask check: no capability, no operation.
type Capability uint64

const (
	CapVaultStatus Capability = 1 << iota
	CapVaultUnlock
	CapVaultLock
	CapRecordList
	CapRecordRead
	CapRecordReveal
	CapRecordWrite
	CapRecordDelete
	CapRecordMatch
	CapRecordRevealForOrigin
	CapRecordCreateLogin
	CapRecordUpdateLogin
	CapPasswordGenerate
	CapBackupCreate
	CapBackupRestore
	CapClientAdmin
	CapPasswordChange
	CapVaultDestroy
	CapRelay
	CapPair
)

type CapabilitySet uint64

func NewCapabilitySet(caps ...Capability) CapabilitySet {
	var s CapabilitySet
	for _, c := range caps {
		s |= CapabilitySet(c)
	}
	return s
}

func (s CapabilitySet) Has(c Capability) bool { return uint64(s)&uint64(c) != 0 }

// capabilityNames gives every bit a stable wire name. Approval prompts show
// these, so an operator reads what a pairing request is asking for.
var capabilityNames = []struct {
	cap  Capability
	name string
}{
	{CapVaultStatus, "vault.status"},
	{CapVaultUnlock, "vault.unlock"},
	{CapVaultLock, "vault.lock"},
	{CapRecordList, "records.list"},
	{CapRecordRead, "records.read"},
	{CapRecordReveal, "records.reveal"},
	{CapRecordWrite, "records.write"},
	{CapRecordDelete, "records.delete"},
	{CapRecordMatch, "records.match"},
	{CapRecordRevealForOrigin, "records.revealForOrigin"},
	{CapRecordCreateLogin, "records.createLogin"},
	{CapRecordUpdateLogin, "records.updateLogin"},
	{CapPasswordGenerate, "password.generate"},
	{CapBackupCreate, "backup.create"},
	{CapBackupRestore, "backup.restore"},
	{CapClientAdmin, "clients.admin"},
	{CapPasswordChange, "vault.changePassword"},
	{CapVaultDestroy, "vault.destroy"},
	{CapRelay, "relay"},
	{CapPair, "pair"},
}

// Names lists the set's capabilities in declaration order.
func (s CapabilitySet) Names() []string {
	out := make([]string, 0, len(capabilityNames))
	for _, c := range capabilityNames {
		if s.Has(c.cap) {
			out = append(out, c.name)
		}
	}
	return out
}

// CLICapabilities is the full administrative set (PRD section 18.1).
var CLICapabilities = NewCapabilitySet(
	CapVaultStatus, CapVaultUnlock, CapVaultLock,
	CapRecordList, CapRecordRead, CapRecordReveal, CapRecordWrite, CapRecordDelete,
	CapRecordMatch, CapRecordRevealForOrigin, CapPasswordGenerate,
	CapBackupCreate, CapBackupRestore,
	CapClientAdmin, CapPasswordChange, CapVaultDestroy,
)

// ChromeCapabilities is the restricted browser set (PRD section 18.2). It
// must never include backup, client-admin, destroy, or unrestricted reveal.
var ChromeCapabilities = NewCapabilitySet(
	CapVaultStatus, CapVaultUnlock, CapVaultLock,
	CapRecordMatch, CapRecordRevealForOrigin,
	CapRecordCreateLogin, CapRecordUpdateLogin,
	CapPasswordGenerate,
)

// RelayCapabilities is all vault-native gets: forwarding frames.
var RelayCapabilities = NewCapabilitySet(CapVaultStatus, CapRelay)

// PairingCapabilities is granted to an unpaired Noise_XX session.
var PairingCapabilities = NewCapabilitySet(CapPair, CapVaultStatus)
