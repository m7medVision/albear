// vaultd is the albear daemon: the single owner of the vault database, keys,
// and lock state. It serves Noise-encrypted requests on a Unix socket.
package main

import (
	"context"
	"fmt"
	"log/slog"
	"os"
	"os/signal"
	"syscall"

	"golang.org/x/sys/unix"

	"albear/internal/daemon"
	"albear/internal/infrastructure/crypto"
	"albear/internal/infrastructure/ipc"
	"albear/internal/infrastructure/sqlite"
	"albear/internal/infrastructure/system"
	"albear/internal/version"
)

func main() {
	// Version print only: the daemon never talks to the network (PRD 2.1).
	if len(os.Args) > 1 && (os.Args[1] == "--version" || os.Args[1] == "version") {
		fmt.Println("vaultd", version.Version)
		return
	}
	if err := run(); err != nil {
		fmt.Fprintln(os.Stderr, "vaultd:", err)
		os.Exit(1)
	}
}

func run() error {
	log := slog.New(slog.NewTextHandler(os.Stderr, nil))

	// Memory hardening: no core dumps from the process that holds keys
	// (PRD 16.8). Best-effort.
	unix.Prctl(unix.PR_SET_DUMPABLE, 0, 0, 0, 0)
	var rlim unix.Rlimit
	unix.Setrlimit(unix.RLIMIT_CORE, &rlim)

	paths, err := system.ResolvePaths()
	if err != nil {
		return err
	}
	if err := paths.Prepare(); err != nil {
		return err
	}

	db, err := sqlite.Open(paths.Database())
	if err != nil {
		return err
	}
	if err := os.Chmod(paths.Database(), 0o600); err != nil {
		return err
	}
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()

	if err := sqlite.Migrate(ctx, db); err != nil {
		return err
	}
	store := sqlite.NewStore(db)
	defer store.Close()

	staticKey, err := system.LoadOrCreateStaticKey(paths.StaticKey())
	if err != nil {
		return err
	}

	server := daemon.New(log, store, paths.Database(), staticKey, crypto.DefaultKDFParams)
	server.OnDestroy(stop)

	ln, err := ipc.Listen(paths.Socket())
	if err != nil {
		return err
	}
	log.Info("vaultd listening", "socket", paths.Socket(), "schema", "v1")

	err = server.Serve(ctx, ln)
	// Always leave locked memory behind on the way out.
	server.Vault().Lock()
	log.Info("vaultd stopped")
	return err
}
