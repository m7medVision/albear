package system

import (
	"encoding/hex"
	"encoding/json"
	"os"

	flynnnoise "github.com/flynn/noise"

	transport "albear/internal/infrastructure/transport/noise"
)

type staticKeyFile struct {
	Public  string `json:"public"`
	Private string `json:"private"`
}

// LoadOrCreateStaticKey returns the X25519 static keypair stored at path,
// generating and persisting one (0600) on first run. It is a transport
// identity key, not vault key material (PRD 12.4).
func LoadOrCreateStaticKey(path string) (flynnnoise.DHKey, error) {
	raw, err := os.ReadFile(path)
	if err == nil {
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
	if err := os.WriteFile(path, raw, 0o600); err != nil {
		return flynnnoise.DHKey{}, err
	}
	return key, nil
}
