package application

import (
	"encoding/json"
	"time"

	domain "albear/internal/records/domain"
	shared "albear/internal/shared/domain"
)

// PayloadVersion tags the serialized payload layout inside each ciphertext.
const PayloadVersion = 1

// metadataDTO is the JSON layout of the encrypted metadata half. The record
// type lives here, encrypted: the records table reveals nothing about what a
// record is.
type metadataDTO struct {
	Type        string   `json:"type"`
	Name        string   `json:"name"`
	Username    string   `json:"username,omitempty"`
	Service     string   `json:"service,omitempty"`
	Environment string   `json:"environment,omitempty"`
	URLs        []string `json:"urls,omitempty"`
	Tags        []string `json:"tags,omitempty"`
	CustomKeys  []string `json:"customKeys,omitempty"`
	CreatedAtMs int64    `json:"createdAtMs"`
	UpdatedAtMs int64    `json:"updatedAtMs"`
}

type secretDTO struct {
	Password  string            `json:"password,omitempty"`
	Notes     string            `json:"notes,omitempty"`
	APIKey    string            `json:"apiKey,omitempty"`
	APISecret string            `json:"apiSecret,omitempty"`
	Custom    map[string]string `json:"custom,omitempty"`
}

func encodeMetadata(t domain.RecordType, m domain.RecordMetadata) ([]byte, error) {
	urls := make([]string, 0, len(m.URLs))
	for _, u := range m.URLs {
		urls = append(urls, u.Raw)
	}
	return json.Marshal(metadataDTO{
		Type: string(t), Name: m.Name, Username: m.Username,
		Service: m.Service, Environment: m.Environment,
		URLs: urls, Tags: m.Tags, CustomKeys: m.CustomKeys,
		CreatedAtMs: m.CreatedAt.UnixMilli(), UpdatedAtMs: m.UpdatedAt.UnixMilli(),
	})
}

func decodeMetadata(b []byte) (domain.RecordType, domain.RecordMetadata, error) {
	var dto metadataDTO
	if err := json.Unmarshal(b, &dto); err != nil {
		return "", domain.RecordMetadata{}, shared.ErrIntegrityFailure
	}
	urls := make([]domain.LoginURL, 0, len(dto.URLs))
	for _, raw := range dto.URLs {
		u, err := domain.NewLoginURL(raw)
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
		Password: shared.NewSecretFromString(dto.Password),
		Notes:    shared.NewSecretFromString(dto.Notes),
		APIKey:   shared.NewSecretFromString(dto.APIKey),
		APISecret: shared.NewSecretFromString(dto.APISecret),
		CustomValues: custom,
	}, nil
}
