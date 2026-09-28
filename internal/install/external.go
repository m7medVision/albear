package install

import (
	"encoding/json"
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"strings"
)

// externalExtensionsDir is the per-user folder, inside the browser's config
// folder, that Chromium-branded builds on Linux scan for <id>.json files.
const externalExtensionsDir = "External Extensions"

// externalPref is the <id>.json Chromium reads from an external-extensions
// folder. Both fields are required on Linux, and the browser only upgrades
// the extension when external_version increases.
type externalPref struct {
	ExternalCRX     string `json:"external_crx"`
	ExternalVersion string `json:"external_version"`
}

// externalPaths is where Install puts the per-user registration: the <id>.json
// and, next to it, the copied <id>.crx it points at. Naming both by ID keeps
// the dev and prod extensions from clobbering each other.
func externalPaths(s BrowserStrategy, id Identity) (jsonPath, crxPath string, err error) {
	config, err := s.ConfigDir()
	if err != nil {
		return "", "", err
	}
	dir := filepath.Join(config, externalExtensionsDir)
	return filepath.Join(dir, id.ExtensionID+".json"), filepath.Join(dir, id.ExtensionID+".crx"), nil
}

// planExtension fills in how the browser will get the extension, without
// writing anything.
func planExtension(s BrowserStrategy, opts Options, id Identity, r *Result) error {
	if r.CRXPath == "" {
		r.Extension = ExtensionManual
		return nil
	}
	if s.SupportsExternalExtensions() {
		jsonPath, _, err := externalPaths(s, id)
		if err != nil {
			return err
		}
		r.Extension = ExtensionRegistered
		r.ExternalPath = jsonPath
		return nil
	}
	dir := s.SystemExtensionsDir()
	if dir == "" {
		r.Extension = ExtensionManual
		return nil
	}
	r.ExternalPath = opts.system(filepath.Join(dir, id.ExtensionID+".json"))
	switch _, err := os.Stat(r.ExternalPath); {
	case err == nil:
		r.Extension = ExtensionSystemPresent
	case errors.Is(err, fs.ErrNotExist):
		r.Extension = ExtensionNeedsSudo
		cmd, err := sudoCommand(r.ExternalPath, externalPref{ExternalCRX: r.CRXPath, ExternalVersion: r.ExtensionVersion})
		if err != nil {
			return err
		}
		r.SudoCommand = cmd
	default:
		return err
	}
	return nil
}

// writeExternal copies the .crx next to jsonPath and then writes the JSON
// pointing at it, so the browser never sees a JSON without its package.
// Re-running replaces both, which is how a newer version gets picked up.
func writeExternal(jsonPath string, pkg crxPackage) error {
	dir := filepath.Dir(jsonPath)
	crxPath := strings.TrimSuffix(jsonPath, ".json") + ".crx"
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return err
	}
	if err := writeFileAtomic(crxPath, pkg.data); err != nil {
		return err
	}
	data, err := json.MarshalIndent(externalPref{ExternalCRX: crxPath, ExternalVersion: pkg.version}, "", "  ")
	if err != nil {
		return err
	}
	return writeFileAtomic(jsonPath, append(data, '\n'))
}

func writeFileAtomic(path string, data []byte) error {
	tmp, err := os.CreateTemp(filepath.Dir(path), "."+filepath.Base(path)+".*")
	if err != nil {
		return err
	}
	defer os.Remove(tmp.Name())
	if _, err := tmp.Write(data); err != nil {
		tmp.Close()
		return err
	}
	if err := tmp.Chmod(0o644); err != nil {
		tmp.Close()
		return err
	}
	if err := tmp.Close(); err != nil {
		return err
	}
	return os.Rename(tmp.Name(), path)
}

// sudoCommand is the one-liner that creates Chrome's root-owned
// external-extension file. Install prints it and never runs it.
func sudoCommand(path string, pref externalPref) (string, error) {
	data, err := json.Marshal(pref)
	if err != nil {
		return "", err
	}
	return fmt.Sprintf("sudo mkdir -p %s && printf '%%s\\n' %s | sudo tee %s >/dev/null",
		shellQuote(filepath.Dir(path)), shellQuote(string(data)), shellQuote(path)), nil
}

// shellQuote single-quotes s for sh, leaving plain paths readable.
func shellQuote(s string) string {
	if s != "" && strings.Trim(s, "abcdefghijklmnopqrstuvwxyzABCDEFGHIJKLMNOPQRSTUVWXYZ0123456789/._-+") == "" {
		return s
	}
	return "'" + strings.ReplaceAll(s, "'", `'\''`) + "'"
}
