package crypto

import "encoding/binary"

// PayloadKind distinguishes the two independently encrypted halves of a record
// inside AEAD associated data, so one can never be interpreted as the other.
type PayloadKind byte

const (
	PayloadMetadata PayloadKind = 1
	PayloadSecret   PayloadKind = 2
	PayloadCanary   PayloadKind = 3
	PayloadLabel    PayloadKind = 4
	PayloadEvent    PayloadKind = 5
	PayloadBackup   PayloadKind = 6
)

// RecordAAD builds the associated data binding a record payload to its vault,
// record, revision, kind, format version, and key version (PRD section 16.5).
// The encoding is fixed-width and versioned by construction: any field change
// produces different bytes and therefore an authentication failure.
func RecordAAD(vaultID, recordID []byte, revision uint64, kind PayloadKind, formatVersion, keyVersion uint32) []byte {
	aad := make([]byte, 0, len(vaultID)+len(recordID)+8+1+4+4)
	aad = append(aad, vaultID...)
	aad = append(aad, recordID...)
	aad = binary.BigEndian.AppendUint64(aad, revision)
	aad = append(aad, byte(kind))
	aad = binary.BigEndian.AppendUint32(aad, formatVersion)
	aad = binary.BigEndian.AppendUint32(aad, keyVersion)
	return aad
}
