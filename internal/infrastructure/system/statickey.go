package system

import (
	"encoding/hex"
	"encoding/json"
	"os"

	flynnnoise "github.com/flynn/noise"

	transport "github.com/m7medVision/albear/internal/infrastructure/transport/noise"
)

type staticKeyFile struct {
	Public  string `json:"public"`
	Private string `json:"private"`
}

// LoadOrCreateStaticKey returns the X25519 static keypair stored at path,
// generating and persisting one (0600) on first run. It is a transport
// identity key, not vault key material (PRD 12.4).
//
// Loading fails closed on loose permissions: the private half is the daemon's
// transport identity, and a group- or world-readable copy means another user
// could already have impersonated the daemon to a pairing client. Creation
// uses O_EXCL so it can neither follow a symlink planted at the path nor
// clobber a key that is already there.
func LoadOrCreateStaticKey(path string) (flynnnoise.DHKey, error) {
	raw, err := os.ReadFile(path)
	if err == nil {
		if err := CheckPrivate(path); err != nil {
			return flynnnoise.DHKey{}, err
		}
		var f staticKeyFile
		if err := json.Unmarshal(raw, &f); err != nil {
			return flynnnoise.DHKey{}, err
		}
		pub, err := hex.DecodeString(f.Public)
		if err != nil {
			return flynnnoise.DHKey{}, err
		}
		priv, err := hex.DecodeString(f.Private)
		if err != nil {
			return flynnnoise.DHKey{}, err
		}
		return flynnnoise.DHKey{Public: pub, Private: priv}, nil
	}
	if !os.IsNotExist(err) {
		return flynnnoise.DHKey{}, err
	}

	key, err := transport.GenerateStaticKey()
	if err != nil {
		return flynnnoise.DHKey{}, err
	}
	raw, err = json.Marshal(staticKeyFile{
		Public:  hex.EncodeToString(key.Public),
		Private: hex.EncodeToString(key.Private),
	})
	if err != nil {
		return flynnnoise.DHKey{}, err
	}
	f, err := os.OpenFile(path, os.O_WRONLY|os.O_CREATE|os.O_EXCL, 0o600)
	if err != nil {
		return flynnnoise.DHKey{}, err
	}
	if _, err := f.Write(raw); err != nil {
		f.Close()
		os.Remove(path)
		return flynnnoise.DHKey{}, err
	}
	if err := f.Close(); err != nil {
		os.Remove(path)
		return flynnnoise.DHKey{}, err
	}
	return key, nil
}
