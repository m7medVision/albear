package domain

import (
	"time"

	shared "github.com/m7medVision/albear/internal/shared/domain"
)

type RecordType string

const (
	TypeLogin         RecordType = "login"
	TypeSecureNote    RecordType = "note"
	TypeAPICredential RecordType = "api"
)

func (t RecordType) Valid() bool {
	switch t {
	case TypeLogin, TypeSecureNote, TypeAPICredential:
		return true
	}
	return false
}

// RecordMetadata is the searchable half of a record. It is encrypted at rest
// but decrypted into the in-memory index while the vault is unlocked.
type RecordMetadata struct {
	Name        string
	Username    string
	Service     string
	Environment string
	URLs        []LoginURL
	Tags        []string
	CustomKeys  []string
	CreatedAt   time.Time
	UpdatedAt   time.Time
}

// SecretPayload is the on-demand half. It is decrypted only for an explicit
// reveal or fill operation, never for search.
type SecretPayload struct {
	Password     shared.SecretString
	Notes        shared.SecretString
	APIKey       shared.SecretString
	APISecret    shared.SecretString
	CustomValues map[string]shared.SecretString
}

// Wipe best-effort clears every secret buffer in the payload.
func (p *SecretPayload) Wipe() {
	p.Password.Wipe()
	p.Notes.Wipe()
	p.APIKey.Wipe()
	p.APISecret.Wipe()
	for _, v := range p.CustomValues {
		v.Wipe()
	}
}

// Record is the secret-record aggregate root.
type Record struct {
	ID       shared.ID
	Type     RecordType
	Revision uint64
	Metadata RecordMetadata
	Secret   SecretPayload
}

// Validate enforces the record invariants from PRD section 10.2.
func (r *Record) Validate() error {
	if r.ID.IsZero() || !r.Type.Valid() || r.Revision < 1 {
		return shared.ErrValidation
	}
	if r.Metadata.Name == "" {
		return shared.ErrValidation
	}
	switch r.Type {
	case TypeLogin:
		// A login needs at least one credential or URL besides its name.
		if r.Secret.Password.IsEmpty() && r.Metadata.Username == "" && len(r.Metadata.URLs) == 0 {
			return shared.ErrValidation
		}
	case TypeAPICredential:
		if r.Secret.APIKey.IsEmpty() && r.Secret.APISecret.IsEmpty() {
			return shared.ErrValidation
		}
	}
	for _, u := range r.Metadata.URLs {
		if u.Origin.Host == "" {
			return shared.ErrValidation
		}
	}
	return nil
}

// MatchesOrigin reports whether any of the record's URLs match the page
// origin under the canonical matching policy.
func (r *Record) MatchesOrigin(page CanonicalOrigin) bool {
	for _, u := range r.Metadata.URLs {
		if u.Origin.Matches(page) {
			return true
		}
	}
	return false
}
