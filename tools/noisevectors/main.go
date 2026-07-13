// noisevectors generates deterministic Noise handshake vectors from the Go
// (flynn/noise) implementation, consumed by the extension's TypeScript test
// suite to pin cross-language interoperability (PRD 26.3).
package main

import (
	"crypto/sha256"
	"encoding/binary"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io"
	"os"

	"github.com/flynn/noise"
)

// detReader yields sha256(seed || counter) blocks: a deterministic RNG.
type detReader struct {
	seed    []byte
	counter uint64
	buf     []byte
}

func (d *detReader) Read(p []byte) (int, error) {
	for len(d.buf) < len(p) {
		var ctr [8]byte
		binary.BigEndian.PutUint64(ctr[:], d.counter)
		sum := sha256.Sum256(append(append([]byte{}, d.seed...), ctr[:]...))
		d.buf = append(d.buf, sum[:]...)
		d.counter++
	}
	n := copy(p, d.buf)
	d.buf = d.buf[n:]
	return n, nil
}

func firstBlock(seed string) []byte {
	r := &detReader{seed: []byte(seed)}
	b := make([]byte, 32)
	io.ReadFull(r, b)
	return b
}

type vector struct {
	Name          string   `json:"name"`
	PSK           string   `json:"psk,omitempty"`
	Prologue      string   `json:"prologue"`
	InitStaticPriv string  `json:"initStaticPriv"`
	RespStaticPriv string  `json:"respStaticPriv"`
	RespStaticPub  string  `json:"respStaticPub"`
	InitEphPriv    string  `json:"initEphPriv"`
	Messages       []string `json:"messages"`
	TransportToResp string  `json:"transportToResp"`
	TransportToInit string  `json:"transportToInit"`
	TransportToRespPlain string `json:"transportToRespPlain"`
	TransportToInitPlain string `json:"transportToInitPlain"`
}

func generate(name string, psk []byte) vector {
	cs := noise.NewCipherSuite(noise.DH25519, noise.CipherChaChaPoly, noise.HashSHA256)
	prologue := []byte(`{"v":1,"mode":"test"}`)

	initStatic, err := cs.GenerateKeypair(&detReader{seed: []byte(name + "-init-static")})
	must(err)
	respStatic, err := cs.GenerateKeypair(&detReader{seed: []byte(name + "-resp-static")})
	must(err)

	initRandom := &detReader{seed: []byte(name + "-init-eph")}
	respRandom := &detReader{seed: []byte(name + "-resp-eph")}

	mk := func(initiator bool) *noise.HandshakeState {
		cfg := noise.Config{
			CipherSuite: cs, Pattern: noise.HandshakeXX, Initiator: initiator,
			Prologue: prologue,
		}
		if initiator {
			cfg.StaticKeypair = initStatic
			cfg.Random = initRandom
		} else {
			cfg.StaticKeypair = respStatic
			cfg.Random = respRandom
		}
		if psk != nil {
			cfg.PresharedKey = psk
			cfg.PresharedKeyPlacement = 3
		}
		hs, err := noise.NewHandshakeState(cfg)
		must(err)
		return hs
	}
	ihs, rhs := mk(true), mk(false)

	msg1, _, _, err := ihs.WriteMessage(nil, nil)
	must(err)
	_, _, _, err = rhs.ReadMessage(nil, msg1)
	must(err)
	msg2, _, _, err := rhs.WriteMessage(nil, nil)
	must(err)
	_, _, _, err = ihs.ReadMessage(nil, msg2)
	must(err)
	msg3, iSend, iRecv, err := ihs.WriteMessage(nil, nil)
	must(err)
	_, rRecv, rSend, err := rhs.ReadMessage(nil, msg3)
	must(err)

	// Transport round trips in both directions.
	toRespPlain := []byte("ping-from-initiator")
	toResp, err := iSend.Encrypt(nil, nil, toRespPlain)
	must(err)
	if pt, err := rRecv.Decrypt(nil, nil, toResp); err != nil || string(pt) != string(toRespPlain) {
		panic("go-side transport mismatch")
	}
	toInitPlain := []byte("pong-from-responder")
	toInit, err := rSend.Encrypt(nil, nil, toInitPlain)
	must(err)
	if pt, err := iRecv.Decrypt(nil, nil, toInit); err != nil || string(pt) != string(toInitPlain) {
		panic("go-side transport mismatch")
	}

	v := vector{
		Name:          name,
		Prologue:      hex.EncodeToString(prologue),
		InitStaticPriv: hex.EncodeToString(initStatic.Private),
		RespStaticPriv: hex.EncodeToString(respStatic.Private),
		RespStaticPub:  hex.EncodeToString(respStatic.Public),
		InitEphPriv:    hex.EncodeToString(firstBlock(name + "-init-eph")),
		Messages: []string{
			hex.EncodeToString(msg1), hex.EncodeToString(msg2), hex.EncodeToString(msg3),
		},
		TransportToResp:      hex.EncodeToString(toResp),
		TransportToInit:      hex.EncodeToString(toInit),
		TransportToRespPlain: hex.EncodeToString(toRespPlain),
		TransportToInitPlain: hex.EncodeToString(toInitPlain),
	}
	if psk != nil {
		v.PSK = hex.EncodeToString(psk)
	}
	return v
}

func must(err error) {
	if err != nil {
		panic(err)
	}
}

func main() {
	psk := sha256.Sum256([]byte("vector-psk"))
	vectors := []vector{
		generate("xx", nil),
		generate("xxpsk3", psk[:]),
	}
	out, err := json.MarshalIndent(vectors, "", "  ")
	must(err)
	if len(os.Args) > 1 {
		must(os.WriteFile(os.Args[1], out, 0o644))
		return
	}
	fmt.Println(string(out))
}
