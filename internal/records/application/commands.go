package application

import (
	"context"
	"database/sql"

	"github.com/m7medVision/albear/internal/catalog"
	"github.com/m7medVision/albear/internal/infrastructure/crypto"
	"github.com/m7medVision/albear/internal/infrastructure/sqlite/gen/command"
	domain "github.com/m7medVision/albear/internal/records/domain"
	shared "github.com/m7medVision/albear/internal/shared/domain"
)

// Create validates, encrypts, and persists a new record, then updates the
// index after commit (PRD 15.4).
func (s *Service) Create(ctx context.Context, t domain.RecordType, meta domain.RecordMetadata, secret domain.SecretPayload) (shared.ID, error) {
	kr, err := s.keys.Keys()
	if err != nil {
		return shared.ID{}, err
	}
	idBytes, err := crypto.NewID()
	if err != nil {
		return shared.ID{}, err
	}
	id, _ := shared.IDFromBytes(idBytes)

	now := s.clock.Now()
	meta.CreatedAt = now
	meta.UpdatedAt = now

	rec := &domain.Record{ID: id, Type: t, Revision: 1, Metadata: meta, Secret: secret}
	if err := rec.Validate(); err != nil {
		return shared.ID{}, err
	}

	metaCT, metaNonce, secCT, secNonce, err := s.encryptRecord(kr, rec)
	if err != nil {
		return shared.ID{}, err
	}
	_, _, keyVersion, err := s.keys.VaultInfo()
	if err != nil {
		return shared.ID{}, err
	}

	err = s.store.CommandTx(ctx, func(tx *sql.Tx, c *command.Queries) error {
		if err := c.InsertRecord(ctx, command.InsertRecordParams{
			RecordID: idBytes, KeyVersion: int64(keyVersion), Revision: 1,
			MetadataNonce: metaNonce, MetadataCiphertext: metaCT,
			SecretNonce: secNonce, SecretCiphertext: secCT,
			PayloadVersion: PayloadVersion,
		}); err != nil {
			return err
		}
		return s.stamp(ctx, tx, kr)
	})
	if err != nil {
		return shared.ID{}, err
	}

	s.index.Put(&IndexEntry{ID: id, Type: t, Revision: 1, Metadata: meta})
	return id, nil
}

// Update applies optimistic concurrency: the caller's expected revision must
// still be current or the write is rejected (PRD 15.5). Fresh nonces always.
func (s *Service) Update(ctx context.Context, id shared.ID, expectedRevision uint64, meta domain.RecordMetadata, secret domain.SecretPayload) error {
	kr, err := s.keys.Keys()
	if err != nil {
		return err
	}
	entry, ok := s.index.Get(id)
	if !ok {
		return shared.ErrRecordNotFound
	}

	newRevision := expectedRevision + 1
	meta.CreatedAt = entry.Metadata.CreatedAt
	meta.UpdatedAt = s.clock.Now()

	rec := &domain.Record{ID: id, Type: entry.Type, Revision: newRevision, Metadata: meta, Secret: secret}
	if err := rec.Validate(); err != nil {
		return err
	}
	metaCT, metaNonce, secCT, secNonce, err := s.encryptRecord(kr, rec)
	if err != nil {
		return err
	}

	var rows int64
	err = s.store.CommandTx(ctx, func(tx *sql.Tx, c *command.Queries) error {
		var err error
		rows, err = c.UpdateRecord(ctx, command.UpdateRecordParams{
			Revision:      int64(newRevision),
			MetadataNonce: metaNonce, MetadataCiphertext: metaCT,
			SecretNonce: secNonce, SecretCiphertext: secCT,
			PayloadVersion: PayloadVersion,
			RecordID:       id.Bytes(), Revision_2: int64(expectedRevision),
		})
		if err != nil {
			return err
		}
		// A revision conflict changed nothing, so there is nothing to
		// re-stamp; stamping anyway would burn a counter on a no-op.
		if rows == 0 {
			return nil
		}
		return s.stamp(ctx, tx, kr)
	})
	if err != nil {
		return err
	}
	if rows == 0 {
		return shared.ErrRevisionConflict
	}

	s.index.Put(&IndexEntry{ID: id, Type: entry.Type, Revision: newRevision, Metadata: meta})
	return nil
}

// Delete removes a record and its index entry.
func (s *Service) Delete(ctx context.Context, id shared.ID) error {
	kr, err := s.keys.Keys()
	if err != nil {
		return err
	}
	var rows int64
	err = s.store.CommandTx(ctx, func(tx *sql.Tx, c *command.Queries) error {
		var err error
		rows, err = c.DeleteRecord(ctx, id.Bytes())
		if err != nil {
			return err
		}
		if rows == 0 {
			return nil
		}
		return s.stamp(ctx, tx, kr)
	})
	if err != nil {
		return err
	}
	if rows == 0 {
		return shared.ErrRecordNotFound
	}
	s.index.Remove(id)
	return nil
}

// stamp re-anchors the vault state inside the caller's transaction. Deletion
// is the case that makes this necessary: a removed row leaves every surviving
// ciphertext valid, so only a hash over the whole set notices it is gone.
func (s *Service) stamp(ctx context.Context, tx *sql.Tx, kr *KeyringRef) error {
	vaultID, _, keyVersion, err := s.keys.VaultInfo()
	if err != nil {
		return err
	}
	return catalog.Stamp(ctx, tx, kr.Catalog, vaultID, keyVersion, s.clock.Now())
}

// encryptRecord serializes and encrypts both halves with independent fresh
// nonces and identity-binding AADs.
func (s *Service) encryptRecord(kr *KeyringRef, rec *domain.Record) (metaCT, metaNonce, secCT, secNonce []byte, err error) {
	vaultID, formatVersion, keyVersion, err := s.keys.VaultInfo()
	if err != nil {
		return nil, nil, nil, nil, err
	}

	metaPlain, err := encodeMetadata(rec.Type, rec.Metadata)
	if err != nil {
		return nil, nil, nil, nil, err
	}
	defer crypto.Zero(metaPlain)
	secPlain, err := encodeSecret(rec.Secret)
	if err != nil {
		return nil, nil, nil, nil, err
	}
	defer crypto.Zero(secPlain)

	metaNonce, err = crypto.NewNonce()
	if err != nil {
		return nil, nil, nil, nil, err
	}
	secNonce, err = crypto.NewNonce()
	if err != nil {
		return nil, nil, nil, nil, err
	}

	recID := rec.ID.Bytes()
	metaCT, err = crypto.Seal(kr.Metadata, metaNonce,
		metaPlain, crypto.RecordAAD(vaultID, recID, rec.Revision, crypto.PayloadMetadata, formatVersion, keyVersion))
	if err != nil {
		return nil, nil, nil, nil, err
	}
	secCT, err = crypto.Seal(kr.Secret, secNonce,
		secPlain, crypto.RecordAAD(vaultID, recID, rec.Revision, crypto.PayloadSecret, formatVersion, keyVersion))
	if err != nil {
		return nil, nil, nil, nil, err
	}
	return metaCT, metaNonce, secCT, secNonce, nil
}
