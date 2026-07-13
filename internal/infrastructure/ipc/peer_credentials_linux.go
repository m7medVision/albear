//go:build linux

// Package ipc provides Unix-domain-socket helpers: peer credential checks and
// hardened listener setup (PRD 12.1).
package ipc

import (
	"errors"
	"fmt"
	"net"
	"os"

	"golang.org/x/sys/unix"
)

var ErrPeerDenied = errors.New("ipc: peer user is not the daemon user")

// VerifyPeer checks via SO_PEERCRED that the connecting process runs as the
// same user as the daemon.
func VerifyPeer(conn *net.UnixConn) error {
	raw, err := conn.SyscallConn()
	if err != nil {
		return err
	}
	var cred *unix.Ucred
	var credErr error
	if err := raw.Control(func(fd uintptr) {
		cred, credErr = unix.GetsockoptUcred(int(fd), unix.SOL_SOCKET, unix.SO_PEERCRED)
	}); err != nil {
		return err
	}
	if credErr != nil {
		return credErr
	}
	if cred.Uid != uint32(os.Getuid()) {
		return fmt.Errorf("%w: uid %d", ErrPeerDenied, cred.Uid)
	}
	return nil
}

// Listen creates the Unix socket with 0600 permissions, replacing any stale
// socket file from a previous run.
func Listen(path string) (*net.UnixListener, error) {
	os.Remove(path)
	addr, err := net.ResolveUnixAddr("unix", path)
	if err != nil {
		return nil, err
	}
	ln, err := net.ListenUnix("unix", addr)
	if err != nil {
		return nil, err
	}
	if err := os.Chmod(path, 0o600); err != nil {
		ln.Close()
		return nil, err
	}
	return ln, nil
}
