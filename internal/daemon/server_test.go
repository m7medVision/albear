//go:build linux

package daemon

import (
	"context"
	"encoding/hex"
	"errors"
	"net"
	"path/filepath"
	"testing"

	"github.com/m7medVision/albear/internal/adapters/protocol"
	"github.com/m7medVision/albear/internal/client"
	"github.com/m7medVision/albear/internal/infrastructure/crypto"
	"github.com/m7medVision/albear/internal/infrastructure/ipc"
	"github.com/m7medVision/albear/internal/infrastructure/sqlite"
	transport "github.com/m7medVision/albear/internal/infrastructure/transport/noise"
)

var fastParams = crypto.KDFParams{MemoryKiB: crypto.MinMemoryKiB, Iterations: 3, Parallelism: 4}

type testDaemon struct {
	server *Server
	socket string
	dbPath string
	cancel context.CancelFunc
}

func startDaemon(t *testing.T) *testDaemon {
	t.Helper()
	dir := t.TempDir()
	dbPath := filepath.Join(dir, "vault.db")
	db, err := sqlite.Open(dbPath)
	if err != nil {
		t.Fatal(err)
	}
	ctx0 := context.Background()
	if err := sqlite.Migrate(ctx0, db); err != nil {
		t.Fatal(err)
	}
	store := sqlite.NewStore(db)
	staticKey, err := transport.GenerateStaticKey()
	if err != nil {
		t.Fatal(err)
	}
	server := New(nil, store, dbPath, staticKey, fastParams)

	socket := filepath.Join(dir, "vault.sock")
	ln, err := ipc.Listen(socket)
	if err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithCancel(ctx0)
	go server.Serve(ctx, ln)
	t.Cleanup(func() { cancel(); store.Close() })
	return &testDaemon{server: server, socket: socket, dbPath: dbPath, cancel: cancel}
}

func cliConn(t *testing.T, d *testDaemon) *client.Client {
	t.Helper()
	c, err := client.DialCLI(d.socket)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { c.Close() })
	return c
}

func initAndUnlock(t *testing.T, c *client.Client) {
	t.Helper()
	if err := c.Call("vault.init", map[string]string{"password": "master password"}, nil); err != nil {
		t.Fatal(err)
	}
	if err := c.Call("vault.unlock", map[string]string{"password": "master password"}, nil); err != nil {
		t.Fatal(err)
	}
}

func apiCode(t *testing.T, err error) string {
	t.Helper()
	var ae *client.APIError
	if !errors.As(err, &ae) {
		t.Fatalf("not an API error: %v", err)
	}
	return ae.Code
}

func TestFullLifecycleOverSocket(t *testing.T) {
	d := startDaemon(t)
	c := cliConn(t, d)

	var status struct {
		Initialized bool  `json:"initialized"`
		Unlocked    bool  `json:"unlocked"`
		RecordCount int64 `json:"recordCount"`
	}
	if err := c.Call("vault.status", nil, &status); err != nil {
		t.Fatal(err)
	}
	if status.Initialized {
		t.Fatal("fresh daemon reports initialized")
	}

	initAndUnlock(t, c)

	// Create.
	var created struct {
		ID string `json:"id"`
	}
	err := c.Call("records.create", map[string]any{
		"type": "login", "name": "GitHub", "username": "mo",
		"urls": []string{"https://github.com"}, "password": "hunter2",
	}, &created)
	if err != nil || created.ID == "" {
		t.Fatal(err)
	}

	// List + search redact secrets.
	var list struct {
		Records []map[string]any `json:"records"`
	}
	if err := c.Call("records.list", nil, &list); err != nil || len(list.Records) != 1 {
		t.Fatal(err)
	}
	if _, hasPassword := list.Records[0]["password"]; hasPassword {
		t.Fatal("list leaked a secret field")
	}

	// Reveal.
	var secret struct {
		Password string `json:"password"`
	}
	if err := c.Call("records.reveal", map[string]string{"ref": "github"}, &secret); err != nil {
		t.Fatal(err)
	}
	if secret.Password != "hunter2" {
		t.Fatal("reveal mismatch")
	}

	// Update with revision.
	if err := c.Call("records.update", map[string]any{
		"id": created.ID, "expectedRevision": 1,
		"name": "GitHub", "username": "mo", "urls": []string{"https://github.com"},
		"password": "hunter3",
	}, nil); err != nil {
		t.Fatal(err)
	}
	// Stale revision conflicts.
	err = c.Call("records.update", map[string]any{
		"id": created.ID, "expectedRevision": 1,
		"name": "GitHub", "username": "mo", "password": "x",
	}, nil)
	if apiCode(t, err) != protocol.CodeConflict {
		t.Fatal(err)
	}

	// Match.
	var match struct {
		Records []map[string]any `json:"records"`
	}
	if err := c.Call("records.match", map[string]string{"origin": "https://www.github.com"}, &match); err != nil || len(match.Records) != 1 {
		t.Fatalf("%v %d", err, len(match.Records))
	}
	c.Call("records.match", map[string]string{"origin": "https://github.com.evil.example"}, &match)
	if len(match.Records) != 0 {
		t.Fatal("lookalike matched")
	}

	// Generate password.
	var gen struct {
		Password string `json:"password"`
	}
	if err := c.Call("password.generate", map[string]any{"length": 32}, &gen); err != nil || len(gen.Password) != 32 {
		t.Fatal(err)
	}

	// Delete.
	if err := c.Call("records.delete", map[string]string{"id": created.ID}, nil); err != nil {
		t.Fatal(err)
	}

	// Lock: record operations now fail with VAULT_LOCKED.
	if err := c.Call("vault.lock", nil, nil); err != nil {
		t.Fatal(err)
	}
	err = c.Call("records.list", nil, nil)
	if apiCode(t, err) != protocol.CodeVaultLocked {
		t.Fatal(err)
	}
}

func TestWrongPasswordAndRateLimit(t *testing.T) {
	d := startDaemon(t)
	c := cliConn(t, d)
	if err := c.Call("vault.init", map[string]string{"password": "master password"}, nil); err != nil {
		t.Fatal(err)
	}
	for i := 0; i < 4; i++ {
		err := c.Call("vault.unlock", map[string]string{"password": "wrong"}, nil)
		if apiCode(t, err) != protocol.CodeAuthFailed {
			t.Fatal(err)
		}
	}
	// Attempt 5 arrives inside the backoff window.
	err := c.Call("vault.unlock", map[string]string{"password": "master password"}, nil)
	if apiCode(t, err) != protocol.CodeRateLimited {
		t.Fatal(err)
	}
}

func TestPairingFlowEndToEnd(t *testing.T) {
	d := startDaemon(t)
	cli := cliConn(t, d)
	initAndUnlock(t, cli)

	// Extension generates its static key and requests pairing on an
	// unpaired channel.
	extKey, _ := transport.GenerateStaticKey()
	pairConn, err := client.DialPairing(d.socket, extKey)
	if err != nil {
		t.Fatal(err)
	}
	defer pairConn.Close()

	var pairResp struct {
		PairingID string `json:"pairingId"`
		Phrase    string `json:"phrase"`
	}
	err = pairConn.Call("clients.pair", map[string]any{
		"kind": 2, "label": "chrome@test", "staticKey": hex.EncodeToString(extKey.Public),
	}, &pairResp)
	if err != nil {
		t.Fatal(err)
	}

	// The pairing channel cannot do anything else.
	err = pairConn.Call("records.list", nil, nil)
	if apiCode(t, err) != protocol.CodeDenied {
		t.Fatal(err)
	}

	// CLI sees and approves the request.
	var pending struct {
		Pending []struct {
			PairingID string `json:"pairingId"`
			Phrase    string `json:"phrase"`
		} `json:"pending"`
	}
	if err := cli.Call("clients.pending", nil, &pending); err != nil || len(pending.Pending) != 1 {
		t.Fatal(err)
	}
	if pending.Pending[0].Phrase != pairResp.Phrase {
		t.Fatal("phrase mismatch between channels")
	}
	if err := cli.Call("clients.approve", map[string]string{"pairingId": pairResp.PairingID}, nil); err != nil {
		t.Fatal(err)
	}

	// Extension claims its credential.
	var claim struct {
		ClientID        string `json:"clientId"`
		Credential      string `json:"credential"`
		DaemonStaticKey string `json:"daemonStaticKey"`
	}
	if err := pairConn.Call("clients.claim", map[string]string{"pairingId": pairResp.PairingID}, &claim); err != nil {
		t.Fatal(err)
	}
	credential, _ := hex.DecodeString(claim.Credential)
	daemonKey, _ := hex.DecodeString(claim.DaemonStaticKey)

	// Paired reconnection with PSK and pinned daemon key.
	ext, err := client.DialPaired(d.socket, extKey, claim.ClientID, credential, daemonKey)
	if err != nil {
		t.Fatal(err)
	}
	defer ext.Close()

	// Chrome capabilities: match works…
	if err := ext.Call("records.match", map[string]string{"origin": "https://github.com"}, nil); err != nil {
		t.Fatal(err)
	}
	// …admin/backup/destroy are denied (PRD 18.2, acceptance 13).
	for _, op := range []string{"backup.create", "clients.list", "vault.destroy", "records.list", "records.reveal"} {
		err := ext.Call(op, map[string]string{"path": "/x", "password": "p", "ref": "y"}, nil)
		if apiCode(t, err) != protocol.CodeDenied {
			t.Fatalf("%s: %v", op, err)
		}
	}

	// Wrong credential cannot get service. With psk3 the initiator only
	// learns of the rejection on first use, so dial may succeed locally but
	// the first call must fail.
	bad := make([]byte, 32)
	if badConn, err := client.DialPaired(d.socket, extKey, claim.ClientID, bad, daemonKey); err == nil {
		if err := badConn.Call("vault.status", nil, nil); err == nil {
			t.Fatal("wrong credential was served")
		}
		badConn.Close()
	}

	// Wrong static key cannot get service even with the right credential.
	otherKey, _ := transport.GenerateStaticKey()
	if otherConn, err := client.DialPaired(d.socket, otherKey, claim.ClientID, credential, daemonKey); err == nil {
		if err := otherConn.Call("vault.status", nil, nil); err == nil {
			t.Fatal("unpinned static key was served")
		}
		otherConn.Close()
	}

	// Revocation: existing sessions dropped, reconnection impossible.
	if err := cli.Call("clients.revoke", map[string]string{"id": claim.ClientID}, nil); err != nil {
		t.Fatal(err)
	}
	err = ext.Call("records.match", map[string]string{"origin": "https://github.com"}, nil)
	if apiCode(t, err) != protocol.CodeDenied {
		t.Fatalf("revoked client still served: %v", err)
	}
	if _, err := client.DialPaired(d.socket, extKey, claim.ClientID, credential, daemonKey); err == nil {
		t.Fatal("revoked client reconnected")
	}
}

// TestClaimAfterRehandshake proves the popup's "claim after SW death" path:
// the first pairing connection is closed (simulating the MV3 service worker
// being reaped), then a fresh pair-mode handshake with the same static key
// must succeed in calling clients.claim with the original pairingId.
func TestClaimAfterRehandshake(t *testing.T) {
	d := startDaemon(t)
	cli := cliConn(t, d)
	initAndUnlock(t, cli)

	extKey, _ := transport.GenerateStaticKey()
	pairConn, err := client.DialPairing(d.socket, extKey)
	if err != nil {
		t.Fatal(err)
	}
	var pairResp struct {
		PairingID string `json:"pairingId"`
		Phrase    string `json:"phrase"`
	}
	if err := pairConn.Call("clients.pair", map[string]any{
		"kind": 2, "label": "chrome@test", "staticKey": hex.EncodeToString(extKey.Public),
	}, &pairResp); err != nil {
		t.Fatal(err)
	}
	pairConn.Close()

	if err := cli.Call("clients.approve", map[string]string{"pairingId": pairResp.PairingID}, nil); err != nil {
		t.Fatal(err)
	}

	second, err := client.DialPairing(d.socket, extKey)
	if err != nil {
		t.Fatal(err)
	}
	defer second.Close()

	var claim struct {
		ClientID        string `json:"clientId"`
		Credential      string `json:"credential"`
		DaemonStaticKey string `json:"daemonStaticKey"`
	}
	if err := second.Call("clients.claim", map[string]string{"pairingId": pairResp.PairingID}, &claim); err != nil {
		t.Fatalf("re-handshake claim: %v", err)
	}
	if claim.ClientID == "" || claim.Credential == "" {
		t.Fatal("empty claim response")
	}
}

func TestCancelPairing(t *testing.T) {
	d := startDaemon(t)
	cli := cliConn(t, d)
	initAndUnlock(t, cli)

	extKey, _ := transport.GenerateStaticKey()
	pairConn, err := client.DialPairing(d.socket, extKey)
	if err != nil {
		t.Fatal(err)
	}
	defer pairConn.Close()
	var pairResp struct {
		PairingID string `json:"pairingId"`
	}
	if err := pairConn.Call("clients.pair", map[string]any{
		"kind": 2, "label": "chrome@test", "staticKey": hex.EncodeToString(extKey.Public),
	}, &pairResp); err != nil {
		t.Fatal(err)
	}

	if err := pairConn.Call("clients.cancel", map[string]string{"pairingId": pairResp.PairingID}, nil); err != nil {
		t.Fatal(err)
	}
	var pending struct {
		Pending []struct {
			PairingID string `json:"pairingId"`
		} `json:"pending"`
	}
	cli.Call("clients.pending", nil, &pending)
	if len(pending.Pending) != 0 {
		t.Fatalf("cancel did not remove pending: %+v", pending.Pending)
	}

	if err := pairConn.Call("clients.cancel", map[string]string{"pairingId": pairResp.PairingID}, nil); apiCode(t, err) != protocol.CodeNotFound {
		t.Fatalf("second cancel: %v", err)
	}

	if err := pairConn.Call("clients.cancel", map[string]string{"pairingId": "not-hex"}, nil); apiCode(t, err) != protocol.CodeInvalid {
		t.Fatalf("invalid id: %v", err)
	}

	if err := pairConn.Call("clients.cancel", map[string]string{"pairingId": "0102030405060708090a0b0c0d0e0f10"}, nil); apiCode(t, err) != protocol.CodeNotFound {
		t.Fatalf("cancel before pair: %v", err)
	}
}

func TestRevealForOriginPolicy(t *testing.T) {
	d := startDaemon(t)
	cli := cliConn(t, d)
	initAndUnlock(t, cli)

	var created struct {
		ID string `json:"id"`
	}
	cli.Call("records.create", map[string]any{
		"name": "GitHub", "username": "mo",
		"urls": []string{"https://github.com"}, "password": "pw",
	}, &created)

	var out struct {
		Username string `json:"username"`
		Password string `json:"password"`
	}
	if err := cli.Call("records.revealForOrigin", map[string]string{
		"id": created.ID, "origin": "https://github.com",
	}, &out); err != nil {
		t.Fatal(err)
	}
	if out.Password != "pw" || out.Username != "mo" {
		t.Fatalf("%+v", out)
	}

	err := cli.Call("records.revealForOrigin", map[string]string{
		"id": created.ID, "origin": "https://evilgithub.com",
	}, nil)
	if apiCode(t, err) != protocol.CodeDenied {
		t.Fatal(err)
	}
	err = cli.Call("records.revealForOrigin", map[string]string{
		"id": created.ID, "origin": "http://github.com",
	}, nil)
	if apiCode(t, err) != protocol.CodeDenied {
		t.Fatal("http fill allowed by default")
	}
}

func TestChangePasswordOverSocket(t *testing.T) {
	d := startDaemon(t)
	c := cliConn(t, d)
	initAndUnlock(t, c)
	c.Call("records.create", map[string]any{"name": "n", "type": "note", "notes": "body"}, nil)

	if err := c.Call("vault.changePassword", map[string]string{
		"current": "master password", "next": "new master password",
	}, nil); err != nil {
		t.Fatal(err)
	}
	// Locked after change; old password dead; new one unlocks and records
	// survive without re-encryption.
	err := c.Call("vault.unlock", map[string]string{"password": "master password"}, nil)
	if apiCode(t, err) != protocol.CodeAuthFailed {
		t.Fatal(err)
	}
	if err := c.Call("vault.unlock", map[string]string{"password": "new master password"}, nil); err != nil {
		t.Fatal(err)
	}
	var secret struct {
		Notes string `json:"notes"`
	}
	if err := c.Call("records.reveal", map[string]string{"ref": "n"}, &secret); err != nil || secret.Notes != "body" {
		t.Fatal(err)
	}
}

func TestBackupRestoreOverSocket(t *testing.T) {
	d := startDaemon(t)
	c := cliConn(t, d)
	initAndUnlock(t, c)
	c.Call("records.create", map[string]any{"name": "keepme", "type": "note", "notes": "precious"}, nil)

	backupPath := filepath.Join(t.TempDir(), "b.abk")
	if err := c.Call("backup.create", map[string]string{"path": backupPath}, nil); err != nil {
		t.Fatal(err)
	}
	if err := c.Call("backup.verify", map[string]string{"path": backupPath}, nil); err != nil {
		t.Fatal(err)
	}

	// Mutate, then restore: the mutation must be gone.
	c.Call("records.create", map[string]any{"name": "extra", "type": "note", "notes": "x"}, nil)
	if err := c.Call("backup.restore", map[string]string{"path": backupPath}, nil); err != nil {
		t.Fatal(err)
	}

	// Restore locks the vault; unlock and check contents on a new connection.
	c2 := cliConn(t, d)
	var status struct {
		Unlocked bool `json:"unlocked"`
	}
	if err := c2.Call("vault.status", nil, &status); err != nil || status.Unlocked {
		t.Fatalf("restored vault not locked: %+v %v", status, err)
	}
	if err := c2.Call("vault.unlock", map[string]string{"password": "master password"}, nil); err != nil {
		t.Fatal(err)
	}
	var list struct {
		Records []map[string]any `json:"records"`
	}
	if err := c2.Call("records.list", nil, &list); err != nil || len(list.Records) != 1 {
		t.Fatalf("restored records: %d %v", len(list.Records), err)
	}
}

func TestDestroyRequiresPassword(t *testing.T) {
	d := startDaemon(t)
	c := cliConn(t, d)
	initAndUnlock(t, c)

	err := c.Call("vault.destroy", map[string]string{"password": "wrong"}, nil)
	if apiCode(t, err) != protocol.CodeAuthFailed {
		t.Fatal(err)
	}
	if err := c.Call("vault.destroy", map[string]string{"password": "master password"}, nil); err != nil {
		t.Fatal(err)
	}
	// Vault gone: status on a fresh connection reports uninitialized.
	c2 := cliConn(t, d)
	var status struct {
		Initialized bool `json:"initialized"`
	}
	if err := c2.Call("vault.status", nil, &status); err != nil {
		t.Fatal(err)
	}
	if status.Initialized {
		t.Fatal("vault survived destroy")
	}
}

func TestMalformedAndUnknownRequests(t *testing.T) {
	d := startDaemon(t)
	c := cliConn(t, d)

	err := c.Call("no.such.operation", nil, nil)
	if apiCode(t, err) != protocol.CodeInvalid {
		t.Fatal(err)
	}
	err = c.Call("records.show", map[string]int{"ref": 5}, nil)
	if apiCode(t, err) != protocol.CodeInvalid {
		t.Fatal(err)
	}
}

func TestRawTCPStyleGarbageDisconnects(t *testing.T) {
	d := startDaemon(t)
	// Raw connection sending garbage instead of a handshake: the daemon must
	// drop it without crashing, and the socket must keep serving.
	nc, err := net.Dial("unix", d.socket)
	if err != nil {
		t.Fatal(err)
	}
	nc.Write([]byte("GET / HTTP/1.1\r\nHost: x\r\n\r\n"))
	nc.Close()

	c := cliConn(t, d)
	if err := c.Call("vault.status", nil, nil); err != nil {
		t.Fatal(err)
	}
}

func TestEventsRecorded(t *testing.T) {
	d := startDaemon(t)
	c := cliConn(t, d)
	initAndUnlock(t, c)

	var events struct {
		Events []struct {
			Code int `json:"code"`
		} `json:"events"`
	}
	if err := c.Call("events.recent", map[string]int{"limit": 10}, &events); err != nil {
		t.Fatal(err)
	}
	if len(events.Events) < 2 {
		t.Fatalf("expected creation+unlock events, got %d", len(events.Events))
	}
}
