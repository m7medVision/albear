// Package application implements the Security Monitoring context: recording
// local security events. Details are encrypted under the audit key while the
// vault is unlocked; locked-state events carry only numeric codes.
package application

import (
	"context"
	"time"

	"github.com/m7medVision/albear/internal/infrastructure/crypto"
	"github.com/m7medVision/albear/internal/infrastructure/sqlite"
	"github.com/m7medVision/albear/internal/infrastructure/sqlite/gen/command"
	domain "github.com/m7medVision/albear/internal/security/domain"
	shared "github.com/m7medVision/albear/internal/shared/domain"
	vaultapp "github.com/m7medVision/albear/internal/vault/application"
)

type Recorder struct {
	store *sqlite.Store
	keys  *vaultapp.Service
	clock shared.Clock
}

func NewRecorder(store *sqlite.Store, keys *vaultapp.Service, clock shared.Clock) *Recorder {
	if clock == nil {
		clock = shared.SystemClock{}
	}
	return &Recorder{store: store, keys: keys, clock: clock}
}

// Record persists one event. details may be empty; when the vault is locked
// any details are dropped rather than stored in plaintext. Recording is
// best-effort by design: it must never turn a rejected request into a crash,
// so the error is returned for logging but callers may ignore it.
func (r *Recorder) Record(ctx context.Context, sev domain.Severity, code domain.EventCode, details string) error {
	var nonce, ct []byte
	if details != "" {
		if kr, err := r.keys.Keys(); err == nil {
			vaultID, _, _, err := r.keys.VaultInfo()
			if err == nil {
				n, err := crypto.NewNonce()
				if err == nil {
					aad := crypto.RecordAAD(vaultID, nil, uint64(code), crypto.PayloadEvent, 1, 0)
					if c, err := crypto.Seal(kr.Audit, n, []byte(details), aad); err == nil {
						nonce, ct = n, c
					}
				}
			}
		}
	}
	return r.store.Command(ctx, func(c *command.Queries) error {
		return c.InsertSecurityEvent(ctx, command.InsertSecurityEventParams{
			OccurredAtMs: r.clock.Now().UnixMilli(),
			Severity:     int64(sev),
			EventCode:    int64(code),
			DetailsNonce: nonce, DetailsCiphertext: ct,
		})
	})
}

// Event is a decoded security event for local audit listing.
type Event struct {
	Sequence   int64
	OccurredAt time.Time
	Severity   domain.Severity
	Code       domain.EventCode
	Details    string
}

// Recent lists the newest events, decrypting details when possible.
func (r *Recorder) Recent(ctx context.Context, limit int64) ([]Event, error) {
	rows, err := r.store.Query().ListSecurityEvents(ctx, limit)
	if err != nil {
		return nil, err
	}
	kr, keysErr := r.keys.Keys()
	var vaultID []byte
	if keysErr == nil {
		vaultID, _, _, _ = r.keys.VaultInfo()
	}
	out := make([]Event, 0, len(rows))
	for _, row := range rows {
		e := Event{
			Sequence:   row.SequenceID,
			OccurredAt: time.UnixMilli(row.OccurredAtMs),
			Severity:   domain.Severity(row.Severity),
			Code:       domain.EventCode(row.EventCode),
		}
		if len(row.DetailsCiphertext) > 0 && keysErr == nil {
			aad := crypto.RecordAAD(vaultID, nil, uint64(row.EventCode), crypto.PayloadEvent, 1, 0)
			if pt, err := crypto.Open(kr.Audit, row.DetailsNonce, row.DetailsCiphertext, aad); err == nil {
				e.Details = string(pt)
			} else {
				e.Details = "(integrity failure)"
			}
		}
		out = append(out, e)
	}
	return out, nil
}
