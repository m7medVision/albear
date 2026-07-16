package daemon

import (
	recordsapp "albear/internal/records/application"
	domain "albear/internal/records/domain"
	shared "albear/internal/shared/domain"
)

// Wire DTOs. Secrets travel as plain strings ONLY inside Noise-encrypted
// payloads and only for operations that explicitly reveal or submit them.

type statusData struct {
	Initialized bool   `json:"initialized"`
	Unlocked    bool   `json:"unlocked"`
	Epoch       uint64 `json:"epoch"`
	RecordCount int64  `json:"recordCount"`
}

type passwordPayload struct {
	Password string `json:"password"`
}

type changePasswordPayload struct {
	Current string `json:"current"`
	Next    string `json:"next"`
}

type recordFields struct {
	Type        string            `json:"type,omitempty"`
	Name        string            `json:"name"`
	Username    string            `json:"username,omitempty"`
	Service     string            `json:"service,omitempty"`
	Environment string            `json:"environment,omitempty"`
	URLs        []string          `json:"urls,omitempty"`
	Tags        []string          `json:"tags,omitempty"`
	Password    string            `json:"password,omitempty"`
	Notes       string            `json:"notes,omitempty"`
	APIKey      string            `json:"apiKey,omitempty"`
	APISecret   string            `json:"apiSecret,omitempty"`
	Custom      map[string]string `json:"custom,omitempty"`
}

type updatePayload struct {
	ID               string `json:"id"`
	ExpectedRevision uint64 `json:"expectedRevision"`
	recordFields
}

type refPayload struct {
	Ref string `json:"ref"`
}

type idPayload struct {
	ID string `json:"id"`
}

type queryPayload struct {
	Query string `json:"query"`
}

type originPayload struct {
	Origin string `json:"origin"`
}

type revealForOriginPayload struct {
	ID     string `json:"id"`
	Origin string `json:"origin"`
}

type generatePayload struct {
	Length  int  `json:"length,omitempty"`
	Upper   bool `json:"upper,omitempty"`
	Lower   bool `json:"lower,omitempty"`
	Digits  bool `json:"digits,omitempty"`
	Symbols bool `json:"symbols,omitempty"`
	Default bool `json:"default,omitempty"`
}

type pairPayload struct {
	Kind      int    `json:"kind"`
	Label     string `json:"label"`
	StaticKey string `json:"staticKey"`
}

type pairingIDPayload struct {
	PairingID string `json:"pairingId"`
}

type pathPayload struct {
	Path string `json:"path"`
}

type limitPayload struct {
	Limit int64 `json:"limit,omitempty"`
}

// recordView is the redacted metadata projection returned by list/search/
// match/show. It never contains secret payload fields.
type recordView struct {
	ID          string   `json:"id"`
	Type        string   `json:"type"`
	Revision    uint64   `json:"revision"`
	Name        string   `json:"name"`
	Username    string   `json:"username,omitempty"`
	Service     string   `json:"service,omitempty"`
	Environment string   `json:"environment,omitempty"`
	URLs        []string `json:"urls,omitempty"`
	Tags        []string `json:"tags,omitempty"`
	CreatedAtMs int64    `json:"createdAtMs"`
	UpdatedAtMs int64    `json:"updatedAtMs"`
}

func toRecordView(e *recordsapp.IndexEntry) recordView {
	urls := make([]string, 0, len(e.Metadata.URLs))
	for _, u := range e.Metadata.URLs {
		urls = append(urls, u.Raw)
	}
	return recordView{
		ID: e.ID.String(), Type: string(e.Type), Revision: e.Revision,
		Name: e.Metadata.Name, Username: e.Metadata.Username,
		Service: e.Metadata.Service, Environment: e.Metadata.Environment,
		URLs: urls, Tags: e.Metadata.Tags,
		CreatedAtMs: e.Metadata.CreatedAt.UnixMilli(),
		UpdatedAtMs: e.Metadata.UpdatedAt.UnixMilli(),
	}
}

func toRecordViews(entries []*recordsapp.IndexEntry) []recordView {
	out := make([]recordView, 0, len(entries))
	for _, e := range entries {
		out = append(out, toRecordView(e))
	}
	return out
}

// secretView carries revealed secrets (reveal / revealForOrigin only).
type secretView struct {
	Password  string            `json:"password,omitempty"`
	Notes     string            `json:"notes,omitempty"`
	APIKey    string            `json:"apiKey,omitempty"`
	APISecret string            `json:"apiSecret,omitempty"`
	Custom    map[string]string `json:"custom,omitempty"`
}

func toSecretView(p domain.SecretPayload) secretView {
	custom := map[string]string{}
	for k, v := range p.CustomValues {
		custom[k] = string(v.Expose())
	}
	if len(custom) == 0 {
		custom = nil
	}
	return secretView{
		Password: string(p.Password.Expose()), Notes: string(p.Notes.Expose()),
		APIKey: string(p.APIKey.Expose()), APISecret: string(p.APISecret.Expose()),
		Custom: custom,
	}
}

// fieldsToDomain converts submitted record fields into domain values.
func fieldsToDomain(f recordFields) (domain.RecordMetadata, domain.SecretPayload, error) {
	urls := make([]domain.LoginURL, 0, len(f.URLs))
	for _, raw := range f.URLs {
		u, err := domain.NewLoginURL(raw)
		if err != nil {
			return domain.RecordMetadata{}, domain.SecretPayload{}, err
		}
		urls = append(urls, u)
	}
	var customKeys []string
	custom := map[string]shared.SecretString{}
	for k, v := range f.Custom {
		customKeys = append(customKeys, k)
		custom[k] = shared.NewSecretFromString(v)
	}
	if len(custom) == 0 {
		custom = nil
	}
	meta := domain.RecordMetadata{
		Name: f.Name, Username: f.Username,
		Service: f.Service, Environment: f.Environment,
		URLs: urls, Tags: f.Tags, CustomKeys: customKeys,
	}
	secret := domain.SecretPayload{
		Password:     shared.NewSecretFromString(f.Password),
		Notes:        shared.NewSecretFromString(f.Notes),
		APIKey:       shared.NewSecretFromString(f.APIKey),
		APISecret:    shared.NewSecretFromString(f.APISecret),
		CustomValues: custom,
	}
	return meta, secret, nil
}
