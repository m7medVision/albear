package application

import (
	"bytes"
	"context"
	"errors"
	"path/filepath"
	"testing"
	"time"

	"github.com/m7medVision/albear/internal/infrastructure/crypto"
	"github.com/m7medVision/albear/internal/infrastructure/sqlite"
	domain "github.com/m7medVision/albear/internal/records/domain"
	shared "github.com/m7medVision/albear/internal/shared/domain"
	vaultapp "github.com/m7medVision/albear/internal/vault/application"
)

var fastParams = crypto.KDFParams{MemoryKiB: crypto.MinMemoryKiB, Iterations: 3, Parallelism: 4}

type env struct {
	vault   *vaultapp.Service
	records *Service
	store   *sqlite.Store
}

func newEnv(t *testing.T) *env {
	t.Helper()
	db, err := sqlite.Open(filepath.Join(t.TempDir(), "vault.db"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { db.Close() })
	ctx := context.Background()
	if err := sqlite.Migrate(ctx, db); err != nil {
		t.Fatal(err)
	}
	store := sqlite.NewStore(db)
	vs := vaultapp.NewService(store, nil)
	rs := NewService(store, vs, nil)
	vs.OnLock(rs.ClearIndex)

	if err := vs.Init(ctx, []byte("master"), fastParams); err != nil {
		t.Fatal(err)
	}
	if err := vs.Unlock(ctx, []byte("master")); err != nil {
		t.Fatal(err)
	}
	return &env{vault: vs, records: rs, store: store}
}

func loginMeta(t *testing.T, name, user, url string) domain.RecordMetadata {
	t.Helper()
	u, err := domain.NewLoginURL(url)
	if err != nil {
		t.Fatal(err)
	}
	return domain.RecordMetadata{Name: name, Username: user, URLs: []domain.LoginURL{u}, Tags: []string{"work"}}
}

func TestCreateAndReveal(t *testing.T) {
	e := newEnv(t)
	ctx := context.Background()

	id, err := e.records.Create(ctx, domain.TypeLogin,
		loginMeta(t, "GitHub", "mo", "https://github.com"),
		domain.SecretPayload{Password: shared.NewSecretFromString("hunter2")})
	if err != nil {
		t.Fatal(err)
	}

	// Metadata visible via Show; secret only via Reveal.
	entry, err := e.records.Show(id)
	if err != nil || entry.Metadata.Name != "GitHub" || entry.Revision != 1 {
		t.Fatalf("%+v %v", entry, err)
	}

	payload, err := e.records.Reveal(ctx, id)
	if err != nil {
		t.Fatal(err)
	}
	if string(payload.Password.Expose()) != "hunter2" {
		t.Fatal("password mismatch")
	}
}

func TestCiphertextOnDiskIsOpaque(t *testing.T) {
	e := newEnv(t)
	ctx := context.Background()
	_, err := e.records.Create(ctx, domain.TypeLogin,
		loginMeta(t, "SecretSiteName", "secretuser", "https://github.com"),
		domain.SecretPayload{Password: shared.NewSecretFromString("ultrasecretpw")})
	if err != nil {
		t.Fatal(err)
	}
	rows, err := e.store.Query().ListRecords(ctx)
	if err != nil || len(rows) != 1 {
		t.Fatal(err)
	}
	blob := append(rows[0].MetadataCiphertext, rows[0].SecretCiphertext...)
	for _, needle := range []string{"SecretSiteName", "secretuser", "ultrasecretpw", "github"} {
		if bytes.Contains(blob, []byte(needle)) {
			t.Fatalf("plaintext %q visible in ciphertext", needle)
		}
	}
}

func TestUpdateWithRevisionConflict(t *testing.T) {
	e := newEnv(t)
	ctx := context.Background()
	id, _ := e.records.Create(ctx, domain.TypeLogin,
		loginMeta(t, "Site", "u", "https://site.example"),
		domain.SecretPayload{Password: shared.NewSecretFromString("v1")})

	meta := loginMeta(t, "Site", "u2", "https://site.example")
	if err := e.records.Update(ctx, id, 1, meta, domain.SecretPayload{Password: shared.NewSecretFromString("v2")}); err != nil {
		t.Fatal(err)
	}
	// Same expected revision again → conflict (PRD 15.5).
	err := e.records.Update(ctx, id, 1, meta, domain.SecretPayload{Password: shared.NewSecretFromString("v3")})
	if !errors.Is(err, shared.ErrRevisionConflict) {
		t.Fatalf("want revision conflict, got %v", err)
	}

	entry, _ := e.records.Show(id)
	if entry.Revision != 2 || entry.Metadata.Username != "u2" {
		t.Fatalf("%+v", entry)
	}
	p, _ := e.records.Reveal(ctx, id)
	if string(p.Password.Expose()) != "v2" {
		t.Fatal("update lost")
	}
}

func TestUpdateGeneratesFreshNonces(t *testing.T) {
	e := newEnv(t)
	ctx := context.Background()
	id, _ := e.records.Create(ctx, domain.TypeLogin,
		loginMeta(t, "Site", "u", "https://site.example"),
		domain.SecretPayload{Password: shared.NewSecretFromString("v1")})
	before, _ := e.store.Query().GetRecord(ctx, id.Bytes())

	e.records.Update(ctx, id, 1, loginMeta(t, "Site", "u", "https://site.example"),
		domain.SecretPayload{Password: shared.NewSecretFromString("v2")})
	after, _ := e.store.Query().GetRecord(ctx, id.Bytes())

	if bytes.Equal(before.MetadataNonce, after.MetadataNonce) || bytes.Equal(before.SecretNonce, after.SecretNonce) {
		t.Fatal("nonce reused across update")
	}
}

func TestDelete(t *testing.T) {
	e := newEnv(t)
	ctx := context.Background()
	id, _ := e.records.Create(ctx, domain.TypeSecureNote,
		domain.RecordMetadata{Name: "note"},
		domain.SecretPayload{Notes: shared.NewSecretFromString("body")})
	if err := e.records.Delete(ctx, id); err != nil {
		t.Fatal(err)
	}
	if _, err := e.records.Show(id); !errors.Is(err, shared.ErrRecordNotFound) {
		t.Fatal("deleted record visible")
	}
	if err := e.records.Delete(ctx, id); !errors.Is(err, shared.ErrRecordNotFound) {
		t.Fatal("double delete")
	}
}

func TestSearchAndList(t *testing.T) {
	e := newEnv(t)
	ctx := context.Background()
	e.records.Create(ctx, domain.TypeLogin, loginMeta(t, "GitHub", "mo", "https://github.com"),
		domain.SecretPayload{Password: shared.NewSecretFromString("x")})
	e.records.Create(ctx, domain.TypeLogin, loginMeta(t, "GitLab", "mo", "https://gitlab.com"),
		domain.SecretPayload{Password: shared.NewSecretFromString("y")})
	e.records.Create(ctx, domain.TypeSecureNote, domain.RecordMetadata{Name: "Zebra note"},
		domain.SecretPayload{Notes: shared.NewSecretFromString("n")})

	all, err := e.records.List()
	if err != nil || len(all) != 3 {
		t.Fatalf("%d %v", len(all), err)
	}
	if all[0].Metadata.Name != "GitHub" {
		t.Fatal("list not sorted")
	}

	hits, _ := e.records.Search("github")
	if len(hits) != 1 || hits[0].Metadata.Name != "GitHub" {
		t.Fatalf("search: %d", len(hits))
	}
	hits, _ = e.records.Search("git")
	if len(hits) != 2 {
		t.Fatalf("prefix search: %d", len(hits))
	}
	// Secrets must not be searchable.
	hits, _ = e.records.Search("hunter")
	if len(hits) != 0 {
		t.Fatal("secret matched search")
	}
}

func TestMatchOrigins(t *testing.T) {
	e := newEnv(t)
	ctx := context.Background()
	e.records.Create(ctx, domain.TypeLogin, loginMeta(t, "GitHub", "mo", "https://github.com"),
		domain.SecretPayload{Password: shared.NewSecretFromString("x")})

	hits, err := e.records.Match("https://www.github.com/login")
	if err != nil || len(hits) != 1 {
		t.Fatalf("%d %v", len(hits), err)
	}
	hits, _ = e.records.Match("https://github.com.attacker.example")
	if len(hits) != 0 {
		t.Fatal("lookalike matched")
	}
	if _, err := e.records.Match("garbage"); err == nil {
		t.Fatal("invalid origin accepted")
	}
}

func TestRevealForOrigin(t *testing.T) {
	e := newEnv(t)
	ctx := context.Background()
	id, _ := e.records.Create(ctx, domain.TypeLogin, loginMeta(t, "GitHub", "mo", "https://github.com"),
		domain.SecretPayload{Password: shared.NewSecretFromString("pw")})

	// Matching https origin succeeds.
	p, err := e.records.RevealForOrigin(ctx, id, "https://github.com", false)
	if err != nil || string(p.Password.Expose()) != "pw" {
		t.Fatal(err)
	}
	// Non-matching origin denied.
	if _, err := e.records.RevealForOrigin(ctx, id, "https://evil.example", false); !errors.Is(err, shared.ErrAuthorizationDeny) {
		t.Fatal("cross-origin reveal allowed")
	}
	// HTTP denied by default, allowed with explicit override IF matching.
	if _, err := e.records.RevealForOrigin(ctx, id, "http://github.com", false); !errors.Is(err, shared.ErrAuthorizationDeny) {
		t.Fatal("http reveal allowed by default")
	}
}

func TestLockedVaultDeniesEverything(t *testing.T) {
	e := newEnv(t)
	ctx := context.Background()
	id, _ := e.records.Create(ctx, domain.TypeLogin, loginMeta(t, "GitHub", "mo", "https://github.com"),
		domain.SecretPayload{Password: shared.NewSecretFromString("pw")})

	e.vault.Lock()

	if e.records.Index().Len() != 0 {
		t.Fatal("index survived lock")
	}
	if _, err := e.records.List(); !errors.Is(err, shared.ErrVaultLocked) {
		t.Fatal("list while locked")
	}
	if _, err := e.records.Reveal(ctx, id); !errors.Is(err, shared.ErrVaultLocked) {
		t.Fatal("reveal while locked")
	}
	if _, err := e.records.Create(ctx, domain.TypeSecureNote, domain.RecordMetadata{Name: "x"},
		domain.SecretPayload{Notes: shared.NewSecretFromString("n")}); !errors.Is(err, shared.ErrVaultLocked) {
		t.Fatal("create while locked")
	}
	if err := e.records.Delete(ctx, id); !errors.Is(err, shared.ErrVaultLocked) {
		t.Fatal("delete while locked")
	}
}

func TestLoadIndexRebuildsAfterUnlock(t *testing.T) {
	e := newEnv(t)
	ctx := context.Background()
	id, _ := e.records.Create(ctx, domain.TypeLogin, loginMeta(t, "GitHub", "mo", "https://github.com"),
		domain.SecretPayload{Password: shared.NewSecretFromString("pw")})

	e.vault.Lock()
	if err := e.vault.Unlock(ctx, []byte("master")); err != nil {
		t.Fatal(err)
	}
	if err := e.records.LoadIndex(ctx); err != nil {
		t.Fatal(err)
	}
	entry, err := e.records.Show(id)
	if err != nil || entry.Metadata.Name != "GitHub" || entry.Type != domain.TypeLogin {
		t.Fatalf("%+v %v", entry, err)
	}
	// Timestamps survive the round trip at millisecond precision.
	if entry.Metadata.CreatedAt.IsZero() || time.Since(entry.Metadata.CreatedAt) > time.Hour {
		t.Fatal("created-at lost")
	}
}

func TestTamperedRecordFailsClosed(t *testing.T) {
	e := newEnv(t)
	ctx := context.Background()
	id, _ := e.records.Create(ctx, domain.TypeLogin, loginMeta(t, "GitHub", "mo", "https://github.com"),
		domain.SecretPayload{Password: shared.NewSecretFromString("pw")})

	// Corrupt the secret ciphertext on disk.
	if _, err := e.store.DB().Exec(
		`UPDATE records SET secret_ciphertext = randomblob(length(secret_ciphertext))`); err != nil {
		t.Fatal(err)
	}
	if _, err := e.records.Reveal(ctx, id); !errors.Is(err, shared.ErrIntegrityFailure) {
		t.Fatal("tampered secret revealed")
	}

	// Corrupt metadata: index rebuild must fail closed entirely.
	if _, err := e.store.DB().Exec(
		`UPDATE records SET metadata_ciphertext = randomblob(length(metadata_ciphertext))`); err != nil {
		t.Fatal(err)
	}
	if err := e.records.LoadIndex(ctx); !errors.Is(err, shared.ErrIntegrityFailure) {
		t.Fatal("tampered metadata indexed")
	}
	if e.records.Index().Len() != 0 {
		t.Fatal("partial index left behind")
	}
}

func TestCrossRecordCiphertextSwapDetected(t *testing.T) {
	e := newEnv(t)
	ctx := context.Background()
	idA, _ := e.records.Create(ctx, domain.TypeLogin, loginMeta(t, "A", "a", "https://a.example"),
		domain.SecretPayload{Password: shared.NewSecretFromString("pa")})
	idB, _ := e.records.Create(ctx, domain.TypeLogin, loginMeta(t, "B", "b", "https://b.example"),
		domain.SecretPayload{Password: shared.NewSecretFromString("pb")})

	// Swap B's secret ciphertext+nonce into A's row: AAD binds record IDs, so
	// the swap must be detected.
	rowB, _ := e.store.Query().GetRecord(ctx, idB.Bytes())
	if _, err := e.store.DB().Exec(
		`UPDATE records SET secret_ciphertext = ?, secret_nonce = ? WHERE record_id = ?`,
		rowB.SecretCiphertext, rowB.SecretNonce, idA.Bytes()); err != nil {
		t.Fatal(err)
	}
	if _, err := e.records.Reveal(ctx, idA); !errors.Is(err, shared.ErrIntegrityFailure) {
		t.Fatal("cross-record swap not detected")
	}
}

func TestResolve(t *testing.T) {
	e := newEnv(t)
	ctx := context.Background()
	id, _ := e.records.Create(ctx, domain.TypeLogin, loginMeta(t, "GitHub", "mo", "https://github.com"),
		domain.SecretPayload{Password: shared.NewSecretFromString("pw")})
	e.records.Create(ctx, domain.TypeLogin, loginMeta(t, "GitLab", "mo", "https://gitlab.com"),
		domain.SecretPayload{Password: shared.NewSecretFromString("pw")})

	// By unique name fragment.
	entry, err := e.records.Resolve("github")
	if err != nil || entry.ID != id {
		t.Fatal(err)
	}
	// By full hex ID.
	entry, err = e.records.Resolve(id.String())
	if err != nil || entry.ID != id {
		t.Fatal(err)
	}
	// Ambiguous fragment fails.
	if _, err := e.records.Resolve("git"); !errors.Is(err, shared.ErrRecordNotFound) {
		t.Fatal("ambiguous resolve succeeded")
	}
}

func TestIndexIncrementalMaintenance(t *testing.T) {
	ix := NewIndex()
	u, _ := domain.NewLoginURL("https://site.example")
	id := shared.ID{1}
	ix.Put(&IndexEntry{ID: id, Type: domain.TypeLogin, Revision: 1,
		Metadata: domain.RecordMetadata{Name: "one", URLs: []domain.LoginURL{u}}})

	// Replace with different URL: old host entry must go away.
	u2, _ := domain.NewLoginURL("https://other.example")
	ix.Put(&IndexEntry{ID: id, Type: domain.TypeLogin, Revision: 2,
		Metadata: domain.RecordMetadata{Name: "one", URLs: []domain.LoginURL{u2}}})

	page, _ := domain.ParseOrigin("https://site.example")
	if len(ix.Match(page)) != 0 {
		t.Fatal("stale host index entry")
	}
	page2, _ := domain.ParseOrigin("https://other.example")
	if len(ix.Match(page2)) != 1 {
		t.Fatal("new host missing")
	}
	ix.Remove(id)
	if ix.Len() != 0 || len(ix.Match(page2)) != 0 {
		t.Fatal("remove incomplete")
	}
	// Removing a missing ID is a no-op.
	ix.Remove(shared.ID{9})
}

func TestCodecRoundTrip(t *testing.T) {
	u, _ := domain.NewLoginURL("https://example.com")
	meta := domain.RecordMetadata{
		Name: "n", Username: "u", Service: "svc", Environment: "prod",
		URLs: []domain.LoginURL{u}, Tags: []string{"a", "b"}, CustomKeys: []string{"k"},
		CreatedAt: time.UnixMilli(1000), UpdatedAt: time.UnixMilli(2000),
	}
	b, err := encodeMetadata(domain.TypeAPICredential, meta)
	if err != nil {
		t.Fatal(err)
	}
	typ, got, err := decodeMetadata(b)
	if err != nil || typ != domain.TypeAPICredential {
		t.Fatal(err)
	}
	if got.Name != "n" || got.Service != "svc" || len(got.URLs) != 1 || got.CreatedAt.UnixMilli() != 1000 {
		t.Fatalf("%+v", got)
	}

	sec := domain.SecretPayload{
		Password: shared.NewSecretFromString("p"), APIKey: shared.NewSecretFromString("k"),
		CustomValues: map[string]shared.SecretString{"x": shared.NewSecretFromString("y")},
	}
	sb, err := encodeSecret(sec)
	if err != nil {
		t.Fatal(err)
	}
	gotSec, err := decodeSecret(sb)
	if err != nil {
		t.Fatal(err)
	}
	if string(gotSec.Password.Expose()) != "p" || string(gotSec.CustomValues["x"].Expose()) != "y" {
		t.Fatal("secret round trip mismatch")
	}

	if _, _, err := decodeMetadata([]byte("{bad")); err == nil {
		t.Fatal("bad metadata json accepted")
	}
	if _, err := decodeSecret([]byte("{bad")); err == nil {
		t.Fatal("bad secret json accepted")
	}
	if _, _, err := decodeMetadata([]byte(`{"urls":["::bad::"]}`)); err == nil {
		t.Fatal("bad url in metadata accepted")
	}
}
