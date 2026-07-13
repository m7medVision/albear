//go:build linux

package ipc

import (
	"net"
	"os"
	"path/filepath"
	"testing"
)

func TestListenSetsPermissions(t *testing.T) {
	sock := filepath.Join(t.TempDir(), "t.sock")
	ln, err := Listen(sock)
	if err != nil {
		t.Fatal(err)
	}
	defer ln.Close()
	st, err := os.Stat(sock)
	if err != nil {
		t.Fatal(err)
	}
	if st.Mode().Perm() != 0o600 {
		t.Fatalf("socket mode %v", st.Mode().Perm())
	}
	// Stale socket replacement.
	ln.Close()
	ln2, err := Listen(sock)
	if err != nil {
		t.Fatal(err)
	}
	ln2.Close()
}

func TestVerifyPeerSameUser(t *testing.T) {
	sock := filepath.Join(t.TempDir(), "t.sock")
	ln, err := Listen(sock)
	if err != nil {
		t.Fatal(err)
	}
	defer ln.Close()

	done := make(chan error, 1)
	go func() {
		conn, err := ln.AcceptUnix()
		if err != nil {
			done <- err
			return
		}
		defer conn.Close()
		done <- VerifyPeer(conn)
	}()

	c, err := net.Dial("unix", sock)
	if err != nil {
		t.Fatal(err)
	}
	defer c.Close()
	if err := <-done; err != nil {
		t.Fatalf("same-user peer rejected: %v", err)
	}
}
