package application

import (
	"encoding/json"
	"time"

	domain "github.com/m7medVision/albear/internal/records/domain"
	shared "github.com/m7medVision/albear/internal/shared/domain"
)

// PayloadVersion tags the serialized payload layout inside each ciphertext.
const PayloadVersion = 1

// metadataDTO is the JSON layout of the encrypted metadata half. The record
// type lives here, encrypted: the records table reveals nothing about what a
// record is.
//
// URLs and URLEntries are two generations of the same field. Records written
// before the per-URL subdomain opt-in carry `urls`; everything written now
// carries `urlEntries`. Both are decoded, URLEntries wins, and a legacy `urls`
// entry decodes to AllowSubdomains=false — the secure default, and what
// exact-by-default matching means for records saved under the old policy.
//
// No migration and no PayloadVersion bump: metadata is an opaque BLOB and
// payload_version is not part of the AAD (see crypto/aad.go), so a tolerant
// decoder is all that is needed, and old and new daemons can both read a vault
// mid-upgrade. A record picks up the new encoding the next time it is written.
type metadataDTO struct {
	Type        string     `json:"type"`
	Name        string     `json:"name"`
	Username    string     `json:"username,omitempty"`
	Service     string     `json:"service,omitempty"`
	Environment string     `json:"environment,omitempty"`
	URLs        []string   `json:"urls,omitempty"`
	URLEntries  []urlEntry `json:"urlEntries,omitempty"`
	Tags        []string   `json:"tags,omitempty"`
	CustomKeys  []string   `json:"customKeys,omitempty"`
	CreatedAtMs int64      `json:"createdAtMs"`
	UpdatedAtMs int64      `json:"updatedAtMs"`
}

// urlEntry is one stored URL and its matching policy.
type urlEntry struct {
	URL string `json:"url"`
	// Sub is the subdomain opt-in. omitempty keeps the common (exact) case
	// off the wire, and its absence decoding to false is exactly right.
	Sub bool `json:"sub,omitempty"`
}

type secretDTO struct {
	Password  string            `json:"password,omitempty"`
	Notes     string            `json:"notes,omitempty"`
	APIKey    string            `json:"apiKey,omitempty"`
	APISecret string            `json:"apiSecret,omitempty"`
	Custom    map[string]string `json:"custom,omitempty"`
}

func encodeMetadata(t domain.RecordType, m domain.RecordMetadata) ([]byte, error) {
	// Write only urlEntries. Emitting both would mean two sources of truth for
	// the same URLs, and a reader that preferred the wrong one would silently
	// widen matching.
	entries := make([]urlEntry, 0, len(m.URLs))
	for _, u := range m.URLs {
		entries = append(entries, urlEntry{URL: u.Raw, Sub: u.AllowSubdomains})
	}
	return json.Marshal(metadataDTO{
		Type: string(t), Name: m.Name, Username: m.Username,
		Service: m.Service, Environment: m.Environment,
		URLEntries: entries, Tags: m.Tags, CustomKeys: m.CustomKeys,
		CreatedAtMs: m.CreatedAt.UnixMilli(), UpdatedAtMs: m.UpdatedAt.UnixMilli(),
	})
}

func decodeMetadata(b []byte) (domain.RecordType, domain.RecordMetadata, error) {
	var dto metadataDTO
	if err := json.Unmarshal(b, &dto); err != nil {
		return "", domain.RecordMetadata{}, shared.ErrIntegrityFailure
	}
	// urlEntries supersedes urls. When it is present the legacy field is
	// ignored outright rather than merged: a record written by a current
	// daemon has no urls, and anything claiming both is not something to
	// reconcile.
	entries := dto.URLEntries
	if entries == nil {
		entries = make([]urlEntry, 0, len(dto.URLs))
		for _, raw := range dto.URLs {
			entries = append(entries, urlEntry{URL: raw}) // Sub false: exact
		}
	}
	urls := make([]domain.LoginURL, 0, len(entries))
	for _, e := range entries {
		u, err := domain.NewLoginURLWithPolicy(e.URL, e.Sub)
		if err != nil {
			return "", domain.RecordMetadata{}, shared.ErrIntegrityFailure
		}
		urls = append(urls, u)
	}
	return domain.RecordType(dto.Type), domain.RecordMetadata{
		Name: dto.Name, Username: dto.Username,
		Service: dto.Service, Environment: dto.Environment,
		URLs: urls, Tags: dto.Tags, CustomKeys: dto.CustomKeys,
		CreatedAt: time.UnixMilli(dto.CreatedAtMs), UpdatedAt: time.UnixMilli(dto.UpdatedAtMs),
	}, nil
}

func encodeSecret(p domain.SecretPayload) ([]byte, error) {
	custom := make(map[string]string, len(p.CustomValues))
	for k, v := range p.CustomValues {
		custom[k] = string(v.Expose())
	}
	if len(custom) == 0 {
		custom = nil
	}
	return json.Marshal(secretDTO{
		Password: string(p.Password.Expose()), Notes: string(p.Notes.Expose()),
		APIKey: string(p.APIKey.Expose()), APISecret: string(p.APISecret.Expose()),
		Custom: custom,
	})
}

func decodeSecret(b []byte) (domain.SecretPayload, error) {
	var dto secretDTO
	if err := json.Unmarshal(b, &dto); err != nil {
		return domain.SecretPayload{}, shared.ErrIntegrityFailure
	}
	custom := make(map[string]shared.SecretString, len(dto.Custom))
	for k, v := range dto.Custom {
		custom[k] = shared.NewSecretFromString(v)
	}
	if len(custom) == 0 {
		custom = nil
	}
	return domain.SecretPayload{
		Password:     shared.NewSecretFromString(dto.Password),
		Notes:        shared.NewSecretFromString(dto.Notes),
		APIKey:       shared.NewSecretFromString(dto.APIKey),
		APISecret:    shared.NewSecretFromString(dto.APISecret),
		CustomValues: custom,
	}, nil
}
