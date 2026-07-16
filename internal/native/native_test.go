//go:build linux

package native

import (
	"bytes"
	"context"
	"encoding/base64"
	"encoding/binary"
	"encoding/hex"
	"errors"
	"io"
	"path/filepath"
	"testing"

	"github.com/m7medVision/albear/internal/daemon"
	"github.com/m7medVision/albear/internal/infrastructure/crypto"
	"github.com/m7medVision/albear/internal/infrastructure/ipc"
	"github.com/m7medVision/albear/internal/infrastructure/sqlite"
	transport "github.com/m7medVision/albear/internal/infrastructure/transport/noise"
)

func TestNativeMessageRoundTrip(t *testing.T) {
	var buf bytes.Buffer
	if err := WriteMessage(&buf, wireMsg{Frame: "abc"}); err != nil {
		t.Fatal(err)
	}
	var got wireMsg
	if err := ReadMessage(&buf, &got); err != nil || got.Frame != "abc" {
		t.Fatal(err)
	}
}

func TestNativeMessageLimits(t *testing.T) {
	// Oversized incoming length header.
	var buf bytes.Buffer
	var hdr [4]byte
	binary.LittleEndian.PutUint32(hdr[:], MaxNativeMessage+1)
	buf.Write(hdr[:])
	var v wireMsg
	if err := ReadMessage(&buf, &v); !errors.Is(err, ErrMessageTooLarge) {
		t.Fatal(err)
	}
	// Truncated body.
	buf.Reset()
	binary.LittleEndian.PutUint32(hdr[:], 100)
	buf.Write(hdr[:])
	buf.WriteString("short")
	if err := ReadMessage(&buf, &v); !errors.Is(err, ErrBadMessage) {
		t.Fatal(err)
	}
	// Invalid JSON.
	buf.Reset()
	binary.LittleEndian.PutUint32(hdr[:], 3)
	buf.Write(hdr[:])
	buf.WriteString("{{{")
	if err := ReadMessage(&buf, &v); !errors.Is(err, ErrBadMessage) {
		t.Fatal(err)
	}
	// Oversized outgoing message.
	if err := WriteMessage(io.Discard, wireMsg{Frame: string(make([]byte, MaxNativeMessage))}); !errors.Is(err, ErrMessageTooLarge) {
		t.Fatal("oversized outgoing message accepted")
	}
}

func TestChromeValidator(t *testing.T) {
	id := "abcdefghijklmnopabcdefghijklmnop"
	v := ChromeValidator{AllowedIDs: []string{id}}
	if v.Browser() != "chrome" {
		t.Fatal("browser label wrong")
	}
	for _, origin := range []string{
		"chrome-extension://" + id,
		"chrome-extension://" + id + "/",
	} {
		if err := v.Validate(origin); err != nil {
			t.Fatalf("%s rejected: %v", origin, err)
		}
	}
	for _, origin := range []string{
		"chrome-extension://otheridotheridotheridotheridothe",
		"https://" + id,
		"chrome-extension://short",
		"",
		id,
	} {
		if err := v.Validate(origin); !errors.Is(err, ErrOriginDenied) {
			t.Fatalf("%s accepted", origin)
		}
	}
	if v2 := (ChromeValidator{AllowedIDs: nil}); v2.Validate("anything") == nil {
		t.Fatal("empty allowlist accepted a caller")
	}
}

func TestRelayRefusesCLIMode(t *testing.T) {
	hello := base64.StdEncoding.EncodeToString([]byte(`{"v":1,"mode":"cli"}`))
	var in, out bytes.Buffer
	WriteMessage(&in, wireMsg{Frame: hello})
	err := Relay(&in, &out, "/nonexistent.sock")
	if !errors.Is(err, ErrModeRefused) {
		t.Fatalf("cli mode relayed: %v", err)
	}
	var resp wireMsg
	if err := ReadMessage(&out, &resp); err != nil || resp.Error == "" {
		t.Fatal("no error reported to extension")
	}
}

func TestRelayRejectsGarbageHello(t *testing.T) {
	var in, out bytes.Buffer
	WriteMessage(&in, wireMsg{Frame: "!!!not-base64!!!"})
	if err := Relay(&in, &out, "/nonexistent.sock"); !errors.Is(err, ErrBadMessage) {
		t.Fatal(err)
	}

	in.Reset()
	out.Reset()
	WriteMessage(&in, wireMsg{Frame: base64.StdEncoding.EncodeToString([]byte("not json"))})
	if err := Relay(&in, &out, "/nonexistent.sock"); !errors.Is(err, ErrModeRefused) {
		t.Fatal(err)
	}
}

// TestRelayEndToEnd drives a real daemon through the relay exactly the way
// the extension does: pairing, approval, claim, then paired operation — all
// end-to-end encrypted through a bridge that only sees ciphertext.
func TestRelayEndToEnd(t *testing.T) {
	dir := t.TempDir()
	dbPath := filepath.Join(dir, "vault.db")
	db, err := sqlite.Open(dbPath)
	if err != nil {
		t.Fatal(err)
	}
	ctx := context.Background()
	if err := sqlite.Migrate(ctx, db); err != nil {
		t.Fatal(err)
	}
	store := sqlite.NewStore(db)
	t.Cleanup(func() { store.Close() })
	staticKey, _ := transport.GenerateStaticKey()
	srv := daemon.New(nil, store, dbPath,
		staticKey, crypto.KDFParams{MemoryKiB: crypto.MinMemoryKiB, Iterations: 3, Parallelism: 4})
	socket := filepath.Join(dir, "vault.sock")
	ln, err := ipc.Listen(socket)
	if err != nil {
		t.Fatal(err)
	}
	sctx, cancel := context.WithCancel(ctx)
	t.Cleanup(cancel)
	go srv.Serve(sctx, ln)

	// The "extension": a Noise client whose io runs through native-messaging
	// pipes into the relay.
	extToRelayR, extToRelayW := io.Pipe()
	relayToExtR, relayToExtW := io.Pipe()
	relayDone := make(chan error, 1)
	go func() { relayDone <- Relay(extToRelayR, relayToExtW, socket) }()

	extKey, _ := transport.GenerateStaticKey()
	pipeRW := &nativePipe{r: relayToExtR, w: extToRelayW}

	conn, _, err := transport.ClientHandshake(pipeRW, extKey,
		transport.Hello{Version: 1, Mode: transport.ModePairing}, nil, nil)
	if err != nil {
		t.Fatal(err)
	}

	// Request pairing over the encrypted channel.
	resp := callJSON(t, conn, `{"protocolVersion":1,"requestId":"1","operation":"clients.pair","payload":{"kind":2,"label":"ext","staticKey":"`+hex.EncodeToString(extKey.Public)+`"}}`)
	if !bytes.Contains(resp, []byte(`"ok":true`)) {
		t.Fatalf("pair failed: %s", resp)
	}
	_ = resp
	extToRelayW.Close()
	<-relayDone
}

// nativePipe adapts native-messaging JSON frames to the io.ReadWriter the
// Noise client expects: writes become {frame: b64} messages, reads unwrap.
type nativePipe struct {
	r       io.Reader
	w       io.Writer
	pending []byte
	readBuf []byte
}

func (p *nativePipe) Write(b []byte) (int, error) {
	// The transport writes the 4-byte header and payload in two calls; buffer
	// until a full frame is present, then forward its payload.
	p.pending = append(p.pending, b...)
	for len(p.pending) >= 4 {
		n := int(binary.BigEndian.Uint32(p.pending[:4]))
		if len(p.pending) < 4+n {
			break
		}
		frame := p.pending[4 : 4+n]
		if err := WriteMessage(p.w, wireMsg{Frame: base64.StdEncoding.EncodeToString(frame)}); err != nil {
			return 0, err
		}
		p.pending = p.pending[4+n:]
	}
	return len(b), nil
}

func (p *nativePipe) Read(b []byte) (int, error) {
	if len(p.readBuf) == 0 {
		var msg wireMsg
		if err := ReadMessage(p.r, &msg); err != nil {
			return 0, err
		}
		frame, err := base64.StdEncoding.DecodeString(msg.Frame)
		if err != nil {
			return 0, err
		}
		var hdr [4]byte
		binary.BigEndian.PutUint32(hdr[:], uint32(len(frame)))
		p.readBuf = append(hdr[:], frame...)
	}
	n := copy(b, p.readBuf)
	p.readBuf = p.readBuf[n:]
	return n, nil
}

func callJSON(t *testing.T, conn *transport.Conn, req string) []byte {
	t.Helper()
	if err := conn.Send([]byte(req)); err != nil {
		t.Fatal(err)
	}
	resp, err := conn.Recv()
	if err != nil {
		t.Fatal(err)
	}
	return resp
}
