// crxpack signs an unpacked extension folder into a CRX3 file.
//
//	go run ./tools/crxpack -key extension/keys/dev.pem -out extension/albear.crx extension/dist
//
// With -inspect it instead prints "<id> <version>" for a signed .crx (after
// verifying its signature) or for an unpacked extension folder. The Arch
// PKGBUILD names Chrome's external-extension file from the .crx, so the file
// can never point at a different ID than the package, and checks it against
// the extension it built:
//
//	go run ./tools/crxpack -inspect albear.crx
//	go run ./tools/crxpack -inspect extension/dist
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
	"io"
	"os"
	"path/filepath"

	"github.com/m7medVision/albear/internal/crx"
)

func main() {
	if err := run(os.Args[1:], os.Stdout); err != nil {
		fmt.Fprintln(os.Stderr, "crxpack:", err)
		os.Exit(1)
	}
}

func run(args []string, stdout io.Writer) error {
	fs := flag.NewFlagSet("crxpack", flag.ContinueOnError)
	keyPath := fs.String("key", os.Getenv("ALBEAR_CRX_KEY"), "PEM RSA private key (default $ALBEAR_CRX_KEY)")
	out := fs.String("out", "", "CRX file to write")
	inspect := fs.String("inspect", "", "verify this CRX file and print its extension ID and version")
	if err := fs.Parse(args); err != nil {
		return err
	}
	if *inspect != "" {
		return inspectCRX(*inspect, stdout)
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
	fmt.Fprintf(stdout, "%s: %s %s (extension %s)\n", *out, m.Name, m.Version, crx.ExtensionID(pub))
	return nil
}

// inspectCRX prints "<extension-id> <version>" for a CRX whose signature
// verifies, or for an unpacked extension folder whose manifest pins its key,
// and fails for anything else.
func inspectCRX(path string, stdout io.Writer) error {
	if st, err := os.Stat(path); err == nil && st.IsDir() {
		m, err := crx.ReadManifestFile(path)
		if err != nil {
			return err
		}
		if m.Key == "" {
			return fmt.Errorf("%s: manifest pins no key, so its extension ID is not fixed", path)
		}
		der, err := base64.StdEncoding.DecodeString(m.Key)
		if err != nil {
			return fmt.Errorf("%s: manifest key: %w", path, err)
		}
		fmt.Fprintf(stdout, "%s %s\n", crx.ExtensionID(der), m.Version)
		return nil
	}
	data, err := os.ReadFile(path)
	if err != nil {
		return err
	}
	pkg, err := crx.Verify(data)
	if err != nil {
		return fmt.Errorf("%s: %w", path, err)
	}
	m, err := crx.ReadManifest(pkg.Archive)
	if err != nil {
		return fmt.Errorf("%s: %w", path, err)
	}
	fmt.Fprintf(stdout, "%s %s\n", pkg.ID, m.Version)
	return nil
}
