package noise

import (
	"bytes"
	"crypto/sha256"
	"errors"
	"io"
	"net"
	"testing"
)

func pipePair(t *testing.T) (net.Conn, net.Conn) {
	t.Helper()
	a, b := net.Pipe()
	t.Cleanup(func() { a.Close(); b.Close() })
	return a, b
}

type handshakeResult struct {
	conn   *Conn
	hello  *Hello
	remote []byte
	err    error
}

// runHandshake performs a full client/server handshake over a pipe.
func runHandshake(t *testing.T, hello Hello, psk, pinnedClient, expectedServer []byte, lookupErr error) (client *Conn, server *handshakeResult, serverPub []byte) {
	t.Helper()
	serverKey, err := GenerateStaticKey()
	if err != nil {
		t.Fatal(err)
	}
	clientKey, err := GenerateStaticKey()
	if err != nil {
		t.Fatal(err)
	}
	if pinnedClient != nil && len(pinnedClient) == 0 {
		pinnedClient = clientKey.Public
	}

	ca, cb := pipePair(t)
	resCh := make(chan *handshakeResult, 1)
	go func() {
		conn, h, remote, err := ServerHandshake(cb, serverKey, func(h Hello) ([]byte, []byte, error) {
			if lookupErr != nil {
				return nil, nil, lookupErr
			}
			return psk, pinnedClient, nil
		})
		if err != nil {
			// Unblock a client mid-write on the synchronous pipe.
			cb.Close()
		}
		resCh <- &handshakeResult{conn, h, remote, err}
	}()

	if expectedServer != nil && len(expectedServer) == 0 {
		expectedServer = serverKey.Public
	}
	conn, _, err := ClientHandshake(ca, clientKey, hello, psk, expectedServer)
	if err != nil {
		// Unblock the server side before collecting its result.
		ca.Close()
		res := <-resCh
		if res.err == nil {
			res.err = err
		}
		return nil, res, serverKey.Public
	}
	res := <-resCh
	return conn, res, serverKey.Public
}

func TestPairedHandshakeAndRoundTrip(t *testing.T) {
	psk := sha256.Sum256([]byte("credential"))
	client, server, _ := runHandshake(t,
		Hello{Version: 1, Mode: ModePaired, ClientID: "aabb"},
		psk[:], []byte{}, []byte{}, nil)
	if server.err != nil {
		t.Fatal(server.err)
	}
	if server.hello.ClientID != "aabb" {
		t.Fatal("hello lost")
	}

	// Bidirectional encrypted round trip.
	go func() {
		msg, _ := server.conn.Recv()
		server.conn.Send(append([]byte("echo:"), msg...))
	}()
	if err := client.Send([]byte("hello vault")); err != nil {
		t.Fatal(err)
	}
	reply, err := client.Recv()
	if err != nil || string(reply) != "echo:hello vault" {
		t.Fatalf("%q %v", reply, err)
	}
}

func TestPairingModeNoPSK(t *testing.T) {
	client, server, _ := runHandshake(t, Hello{Version: 1, Mode: ModePairing}, nil, nil, nil, nil)
	if server.err != nil {
		t.Fatal(server.err)
	}
	if len(server.remote) != 32 {
		t.Fatal("client static key not surfaced for pairing")
	}
	go server.conn.Send([]byte("ok"))
	if msg, err := client.Recv(); err != nil || string(msg) != "ok" {
		t.Fatal(err)
	}
}

func TestWrongPSKFailsHandshake(t *testing.T) {
	serverKey, _ := GenerateStaticKey()
	clientKey, _ := GenerateStaticKey()
	ca, cb := pipePair(t)

	serverPSK := sha256.Sum256([]byte("right"))
	clientPSK := sha256.Sum256([]byte("wrong"))

	errCh := make(chan error, 1)
	go func() {
		_, _, _, err := ServerHandshake(cb, serverKey, func(Hello) ([]byte, []byte, error) {
			return serverPSK[:], clientKey.Public, nil
		})
		errCh <- err
	}()
	_, _, err := ClientHandshake(ca, clientKey, Hello{Version: 1, Mode: ModePaired, ClientID: "x"}, clientPSK[:], nil)
	serverErr := <-errCh
	if err == nil && serverErr == nil {
		t.Fatal("wrong PSK completed handshake")
	}
}

func TestPinnedStaticKeyMismatchRejected(t *testing.T) {
	psk := sha256.Sum256([]byte("credential"))
	otherKey, _ := GenerateStaticKey()
	// Server pins a different client key than the one connecting.
	_, server, _ := runHandshake(t,
		Hello{Version: 1, Mode: ModePaired, ClientID: "x"},
		psk[:], otherKey.Public, nil, nil)
	if !errors.Is(server.err, ErrStaticKeyMismatch) {
		t.Fatalf("unpinned client accepted: %v", server.err)
	}
}

func TestClientRejectsWrongServerKey(t *testing.T) {
	psk := sha256.Sum256([]byte("credential"))
	serverKey, _ := GenerateStaticKey()
	clientKey, _ := GenerateStaticKey()
	otherKey, _ := GenerateStaticKey()
	ca, cb := pipePair(t)

	go func() {
		ServerHandshake(cb, serverKey, func(Hello) ([]byte, []byte, error) {
			return psk[:], clientKey.Public, nil
		})
	}()
	// Client pins a different daemon key: it must abort after message 2,
	// before ever sending its own static key.
	_, _, err := ClientHandshake(ca, clientKey,
		Hello{Version: 1, Mode: ModePaired, ClientID: "x"}, psk[:], otherKey.Public)
	ca.Close()
	if !errors.Is(err, ErrStaticKeyMismatch) {
		t.Fatalf("client accepted an impostor daemon: %v", err)
	}
}

func TestLookupErrorAborts(t *testing.T) {
	psk := sha256.Sum256([]byte("credential"))
	sentinel := errors.New("revoked")
	_, server, _ := runHandshake(t,
		Hello{Version: 1, Mode: ModePaired, ClientID: "x"},
		psk[:], []byte{}, nil, sentinel)
	if !errors.Is(server.err, sentinel) {
		t.Fatalf("%v", server.err)
	}
}

func TestBadHelloRejected(t *testing.T) {
	serverKey, _ := GenerateStaticKey()
	ca, cb := pipePair(t)
	errCh := make(chan error, 1)
	go func() {
		_, _, _, err := ServerHandshake(cb, serverKey, nil)
		errCh <- err
	}()
	WriteFrame(ca, []byte("not json"))
	if err := <-errCh; !errors.Is(err, ErrProtocolViolation) {
		t.Fatal(err)
	}

	// Unknown mode.
	ca2, cb2 := pipePair(t)
	go func() {
		_, _, _, err := ServerHandshake(cb2, serverKey, nil)
		errCh <- err
	}()
	WriteFrame(ca2, []byte(`{"v":1,"mode":"bogus"}`))
	if err := <-errCh; !errors.Is(err, ErrUnknownMode) {
		t.Fatal(err)
	}

	// Wrong version.
	ca3, cb3 := pipePair(t)
	go func() {
		_, _, _, err := ServerHandshake(cb3, serverKey, nil)
		errCh <- err
	}()
	WriteFrame(ca3, []byte(`{"v":9,"mode":"pair"}`))
	if err := <-errCh; !errors.Is(err, ErrProtocolViolation) {
		t.Fatal(err)
	}
}

func TestTamperedHelloBreaksHandshake(t *testing.T) {
	// A relay that alters the hello (the prologue) must break the handshake.
	serverKey, _ := GenerateStaticKey()
	clientKey, _ := GenerateStaticKey()
	ca, cb := pipePair(t)

	errCh := make(chan error, 1)
	go func() {
		// Tampering relay: rewrites clientId in the hello, forwards the rest.
		hello, _ := ReadFrame(ca)
		tampered := bytes.Replace(hello, []byte(`"pair"`), []byte(`"pair" `), 1)
		serverSide, clientSide := net.Pipe()
		defer serverSide.Close()
		go func() {
			_, _, _, err := ServerHandshake(clientSide, serverKey, nil)
			errCh <- err
		}()
		WriteFrame(serverSide, tampered)
		// Forward remaining frames both ways, propagating closes so a stalled
		// handshake unwinds instead of deadlocking.
		go func() {
			io.Copy(serverSide, ca)
			serverSide.Close()
		}()
		io.Copy(ca, serverSide)
		ca.Close()
	}()

	_, _, err := ClientHandshake(cb, clientKey, Hello{Version: 1, Mode: ModePairing}, nil, nil)
	// Tear the pipes down so a server still waiting on message 3 unblocks.
	cb.Close()
	ca.Close()
	serverErr := <-errCh
	if err == nil && serverErr == nil {
		t.Fatal("tampered prologue survived handshake")
	}
}

func TestTamperedTransportFrameFailsAEAD(t *testing.T) {
	psk := sha256.Sum256([]byte("credential"))
	serverKey, _ := GenerateStaticKey()
	clientKey, _ := GenerateStaticKey()

	// Real sockets so we can interpose on bytes.
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	defer ln.Close()

	type srv struct {
		conn *Conn
		err  error
	}
	srvCh := make(chan srv, 1)
	go func() {
		c, _ := ln.Accept()
		conn, _, _, err := ServerHandshake(c, serverKey, func(Hello) ([]byte, []byte, error) {
			return psk[:], clientKey.Public, nil
		})
		srvCh <- srv{conn, err}
	}()

	nc, err := net.Dial("tcp", ln.Addr().String())
	if err != nil {
		t.Fatal(err)
	}
	defer nc.Close()
	client, _, err := ClientHandshake(nc, clientKey, Hello{Version: 1, Mode: ModePaired, ClientID: "x"}, psk[:], nil)
	if err != nil {
		t.Fatal(err)
	}
	server := <-srvCh
	if server.err != nil {
		t.Fatal(server.err)
	}

	// Send a tampered ciphertext frame directly on the raw socket.
	ct := make([]byte, 32)
	if err := WriteFrame(nc, ct); err != nil {
		t.Fatal(err)
	}
	if _, err := server.conn.Recv(); !errors.Is(err, ErrTransportAEAD) {
		t.Fatalf("tampered frame accepted: %v", err)
	}
	_ = client
}

func TestRekeyContinuity(t *testing.T) {
	psk := sha256.Sum256([]byte("credential"))
	client, server, _ := runHandshake(t,
		Hello{Version: 1, Mode: ModePaired, ClientID: "x"},
		psk[:], []byte{}, []byte{}, nil)
	if server.err != nil {
		t.Fatal(server.err)
	}

	// Cross the rekey boundary in one direction and verify traffic still
	// authenticates on both sides of the interval.
	done := make(chan error, 1)
	go func() {
		for i := 0; i < RekeyInterval+8; i++ {
			msg, err := server.conn.Recv()
			if err != nil {
				done <- err
				return
			}
			if len(msg) != 5 {
				done <- errors.New("payload corrupted")
				return
			}
		}
		done <- nil
	}()
	for i := 0; i < RekeyInterval+8; i++ {
		if err := client.Send([]byte("ping!")); err != nil {
			t.Fatal(err)
		}
	}
	if err := <-done; err != nil {
		t.Fatal(err)
	}
}

func TestFrameLimits(t *testing.T) {
	var buf bytes.Buffer
	if err := WriteFrame(&buf, make([]byte, MaxFrameSize+1)); !errors.Is(err, ErrFrameTooLarge) {
		t.Fatal("oversized frame written")
	}
	// Oversized length header rejected before allocation.
	buf.Reset()
	buf.Write([]byte{0xFF, 0xFF, 0xFF, 0xFF})
	if _, err := ReadFrame(&buf); !errors.Is(err, ErrFrameTooLarge) {
		t.Fatal("oversized header accepted")
	}
	// Truncated body.
	buf.Reset()
	buf.Write([]byte{0, 0, 0, 10, 1, 2})
	if _, err := ReadFrame(&buf); !errors.Is(err, ErrBadFrame) {
		t.Fatal("truncated frame accepted")
	}
	// Round trip.
	buf.Reset()
	if err := WriteFrame(&buf, []byte("payload")); err != nil {
		t.Fatal(err)
	}
	got, err := ReadFrame(&buf)
	if err != nil || string(got) != "payload" {
		t.Fatal(err)
	}
}

func TestGenerateStaticKey(t *testing.T) {
	k1, err := GenerateStaticKey()
	if err != nil || len(k1.Public) != 32 || len(k1.Private) != 32 {
		t.Fatal(err)
	}
	k2, _ := GenerateStaticKey()
	if bytes.Equal(k1.Public, k2.Public) {
		t.Fatal("duplicate keys")
	}
}

func FuzzReadFrame(f *testing.F) {
	f.Add([]byte{0, 0, 0, 4, 1, 2, 3, 4})
	f.Add([]byte{0xFF, 0xFF, 0xFF, 0xFF})
	f.Add([]byte{})
	f.Fuzz(func(t *testing.T, data []byte) {
		ReadFrame(bytes.NewReader(data)) // must never panic
	})
}

func FuzzServerHandshakeHello(f *testing.F) {
	f.Add([]byte(`{"v":1,"mode":"pair"}`))
	f.Add([]byte(`{"v":1,"mode":"paired","clientId":"aa"}`))
	f.Add([]byte("garbage"))
	serverKey, _ := GenerateStaticKey()
	f.Fuzz(func(t *testing.T, hello []byte) {
		a, b := net.Pipe()
		defer a.Close()
		defer b.Close()
		done := make(chan struct{})
		go func() {
			defer close(done)
			ServerHandshake(b, serverKey, func(Hello) ([]byte, []byte, error) {
				return make([]byte, 32), make([]byte, 32), nil
			})
		}()
		WriteFrame(a, hello)
		a.Close() // handshake sees EOF after the (possibly bad) hello
		<-done
	})
}
