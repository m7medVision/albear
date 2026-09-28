// crxpack signs an unpacked extension folder into a CRX3 file.
//
//	go run ./tools/crxpack -key extension/keys/dev.pem -out extension/albear.crx extension/dist
//
// -key defaults to $ALBEAR_CRX_KEY, so CI can pass the prod key without
// putting its path on the command line. The manifest's pinned "key" must be
// the signing key's public key: a mismatch would install under a different
// extension ID than the one the native host allows, so it is refused.
package main

import (
	"bytes"
	"encoding/base64"
	"errors"
	"flag"
	"fmt"
	"os"
	"path/filepath"

	"github.com/m7medVision/albear/internal/crx"
)

func main() {
	if err := run(os.Args[1:]); err != nil {
		fmt.Fprintln(os.Stderr, "crxpack:", err)
		os.Exit(1)
	}
}

func run(args []string) error {
	fs := flag.NewFlagSet("crxpack", flag.ContinueOnError)
	keyPath := fs.String("key", os.Getenv("ALBEAR_CRX_KEY"), "PEM RSA private key (default $ALBEAR_CRX_KEY)")
	out := fs.String("out", "", "CRX file to write")
	if err := fs.Parse(args); err != nil {
		return err
	}
	if fs.NArg() != 1 || *keyPath == "" || *out == "" {
		return errors.New("usage: crxpack -key KEY.pem -out FILE.crx EXTENSION_DIR")
	}
	dir := fs.Arg(0)

	pemData, err := os.ReadFile(*keyPath)
	if err != nil {
		return err
	}
	key, err := crx.ParsePrivateKey(pemData)
	if err != nil {
		return err
	}
	pub, err := crx.PublicKeyDER(key)
	if err != nil {
		return err
	}
	m, err := crx.ReadManifestFile(dir)
	if err != nil {
		return err
	}
	if m.Key != "" {
		pinned, err := base64.StdEncoding.DecodeString(m.Key)
		if err != nil {
			return fmt.Errorf("manifest key: %w", err)
		}
		if !bytes.Equal(pinned, pub) {
			return fmt.Errorf("manifest pins extension %s but -key signs as %s; build for the matching environment",
				crx.ExtensionID(pinned), crx.ExtensionID(pub))
		}
	}

	archive, err := crx.ZipDir(dir)
	if err != nil {
		return err
	}
	data, err := crx.Pack(archive, key)
	if err != nil {
		return err
	}
	if err := os.MkdirAll(filepath.Dir(*out), 0o755); err != nil {
		return err
	}
	if err := os.WriteFile(*out, data, 0o644); err != nil {
		return err
	}
	fmt.Printf("%s: %s %s (extension %s)\n", *out, m.Name, m.Version, crx.ExtensionID(pub))
	return nil
}
