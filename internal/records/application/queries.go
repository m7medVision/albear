package application

import (
	"context"

	"albear/internal/infrastructure/crypto"
	domain "albear/internal/records/domain"
	shared "albear/internal/shared/domain"
)

// LoadIndex reads every encrypted metadata blob, decrypts metadata only, and
// builds the in-memory index (PRD 15.2 step 8, 17.5). Called at unlock.
func (s *Service) LoadIndex(ctx context.Context) error {
	kr, err := s.keys.Keys()
	if err != nil {
		return err
	}
	vaultID, formatVersion, _, err := s.keys.VaultInfo()
	if err != nil {
		return err
	}

	rows, err := s.store.Query().ListRecords(ctx)
	if err != nil {
		return err
	}
	s.index.Clear()
	for _, row := range rows {
		id, err := shared.IDFromBytes(row.RecordID)
		if err != nil {
			return shared.ErrIntegrityFailure
		}
		aad := crypto.RecordAAD(vaultID, row.RecordID, uint64(row.Revision),
			crypto.PayloadMetadata, formatVersion, uint32(row.KeyVersion))
		plain, err := crypto.Open(kr.Metadata, row.MetadataNonce, row.MetadataCiphertext, aad)
		if err != nil {
			// Tampered metadata: fail closed, never partially display.
			s.index.Clear()
			return shared.ErrIntegrityFailure
		}
		t, meta, err := decodeMetadata(plain)
		crypto.Zero(plain)
		if err != nil {
			s.index.Clear()
			return shared.ErrIntegrityFailure
		}
		s.index.Put(&IndexEntry{ID: id, Type: t, Revision: uint64(row.Revision), Metadata: meta})
	}
	return nil
}

// List returns all metadata entries sorted by name.
func (s *Service) List() ([]*IndexEntry, error) {
	if _, err := s.keys.Keys(); err != nil {
		return nil, err
	}
	return s.index.All(), nil
}

// Search matches the query against indexed metadata only.
func (s *Service) Search(query string) ([]*IndexEntry, error) {
	if _, err := s.keys.Keys(); err != nil {
		return nil, err
	}
	return s.index.Search(query), nil
}

// Match returns login entries matching a page origin (PRD 13.3). Secrets are
// never part of match results.
func (s *Service) Match(rawOrigin string) ([]*IndexEntry, error) {
	if _, err := s.keys.Keys(); err != nil {
		return nil, err
	}
	origin, err := domain.ParseOrigin(rawOrigin)
	if err != nil {
		return nil, err
	}
	return s.index.Match(origin), nil
}

// Show returns one entry's metadata.
func (s *Service) Show(id shared.ID) (*IndexEntry, error) {
	if _, err := s.keys.Keys(); err != nil {
		return nil, err
	}
	e, ok := s.index.Get(id)
	if !ok {
		return nil, shared.ErrRecordNotFound
	}
	return e, nil
}

// Resolve finds a single record by ID prefix or unique name match, for CLI
// ergonomics ("vault show github").
func (s *Service) Resolve(ref string) (*IndexEntry, error) {
	if _, err := s.keys.Keys(); err != nil {
		return nil, err
	}
	if id, err := shared.IDFromString(ref); err == nil {
		if e, ok := s.index.Get(id); ok {
			return e, nil
		}
	}
	matches := s.index.Search(ref)
	if len(matches) == 1 {
		return matches[0], nil
	}
	return nil, shared.ErrRecordNotFound
}

// Reveal decrypts and returns one record's secret payload. This is the only
// query that touches the secret ciphertext (PRD 17.5, 13.4).
func (s *Service) Reveal(ctx context.Context, id shared.ID) (domain.SecretPayload, error) {
	kr, err := s.keys.Keys()
	if err != nil {
		return domain.SecretPayload{}, err
	}
	vaultID, formatVersion, _, err := s.keys.VaultInfo()
	if err != nil {
		return domain.SecretPayload{}, err
	}
	row, err := s.store.Query().GetRecord(ctx, id.Bytes())
	if err != nil {
		return domain.SecretPayload{}, shared.ErrRecordNotFound
	}
	aad := crypto.RecordAAD(vaultID, row.RecordID, uint64(row.Revision),
		crypto.PayloadSecret, formatVersion, uint32(row.KeyVersion))
	plain, err := crypto.Open(kr.Secret, row.SecretNonce, row.SecretCiphertext, aad)
	if err != nil {
		return domain.SecretPayload{}, shared.ErrIntegrityFailure
	}
	payload, err := decodeSecret(plain)
	crypto.Zero(plain)
	if err != nil {
		return domain.SecretPayload{}, shared.ErrIntegrityFailure
	}
	return payload, nil
}

// RevealForOrigin releases a secret only when the record actually matches the
// requesting page origin — the constrained reveal the extension gets
// (PRD 13.4, 18.2). HTTP origins are refused unless explicitly allowed.
func (s *Service) RevealForOrigin(ctx context.Context, id shared.ID, rawOrigin string, allowInsecure bool) (domain.SecretPayload, error) {
	if _, err := s.keys.Keys(); err != nil {
		return domain.SecretPayload{}, err
	}
	origin, err := domain.ParseOrigin(rawOrigin)
	if err != nil {
		return domain.SecretPayload{}, err
	}
	if !origin.IsSecure() && !allowInsecure {
		return domain.SecretPayload{}, shared.ErrAuthorizationDeny
	}
	e, ok := s.index.Get(id)
	if !ok {
		return domain.SecretPayload{}, shared.ErrRecordNotFound
	}
	rec := &domain.Record{ID: e.ID, Type: e.Type, Revision: e.Revision, Metadata: e.Metadata}
	if !rec.MatchesOrigin(origin) {
		return domain.SecretPayload{}, shared.ErrAuthorizationDeny
	}
	return s.Reveal(ctx, id)
}
