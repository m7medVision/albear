// Package crx writes and verifies CRX3 extension packages, the signed format
// Chromium-family browsers accept for externally installed extensions.
//
// A CRX3 file is (components/crx_file/crx3.proto):
//
//	"Cr24" | le32 version (3) | le32 header size N | N-byte CrxFileHeader | ZIP
//
// The header is a protobuf CrxFileHeader carrying one sha256_with_rsa proof
// (public key + signature) and signed_header_data, the encoded SignedData
// {crx_id}. The signature is RSASSA-PKCS1-v1_5 SHA-256 over
//
//	"CRX3 SignedData\x00" | le32 len(signed_header_data) | signed_header_data | ZIP
//
// and crx_id is the first 16 bytes of SHA-256 over the public key's DER
// SubjectPublicKeyInfo, which is also where the extension ID comes from. The
// protobuf encoding is written by hand to stay stdlib-only.
package crx

import (
	"bytes"
	"crypto"
	"crypto/rand"
	"crypto/rsa"
	"crypto/sha256"
	"crypto/x509"
	"encoding/binary"
	"encoding/hex"
	"encoding/pem"
	"errors"
	"fmt"
	"strings"
)

const (
	magic            = "Cr24"
	formatVersion    = 3
	signatureContext = "CRX3 SignedData\x00"

	// CrxFileHeader and SignedData field numbers.
	fieldSHA256WithRSA    = 2
	fieldSignedHeaderData = 10000
	fieldProofPublicKey   = 1
	fieldProofSignature   = 2
	fieldSignedDataCrxID  = 1

	// maxHeaderSize bounds the header Verify will read, like Chromium does.
	maxHeaderSize = 1 << 20
)

// ExtensionID derives a Chromium extension ID from a DER SubjectPublicKeyInfo:
// the first 16 bytes of its SHA-256, hex-encoded with 0-f mapped to a-p.
func ExtensionID(publicKeyDER []byte) string {
	sum := sha256.Sum256(publicKeyDER)
	return idFromCrxID(sum[:16])
}

func idFromCrxID(crxID []byte) string {
	h := hex.EncodeToString(crxID)
	var b strings.Builder
	for _, c := range h {
		if c >= 'a' {
			b.WriteRune('k' + (c - 'a'))
		} else {
			b.WriteRune('a' + (c - '0'))
		}
	}
	return b.String()
}

// ParsePrivateKey reads an RSA private key from PEM (PKCS#8 or PKCS#1).
// Text around the PEM block is ignored.
func ParsePrivateKey(pemData []byte) (*rsa.PrivateKey, error) {
	block, _ := pem.Decode(pemData)
	if block == nil {
		return nil, errors.New("crx: no PEM block in key")
	}
	switch block.Type {
	case "PRIVATE KEY":
		k, err := x509.ParsePKCS8PrivateKey(block.Bytes)
		if err != nil {
			return nil, fmt.Errorf("crx: parse key: %w", err)
		}
		rk, ok := k.(*rsa.PrivateKey)
		if !ok {
			return nil, errors.New("crx: key is not RSA")
		}
		return rk, nil
	case "RSA PRIVATE KEY":
		k, err := x509.ParsePKCS1PrivateKey(block.Bytes)
		if err != nil {
			return nil, fmt.Errorf("crx: parse key: %w", err)
		}
		return k, nil
	}
	return nil, fmt.Errorf("crx: unsupported PEM block %q", block.Type)
}

// PublicKeyDER is the DER SubjectPublicKeyInfo of key's public half, the form
// the manifest "key" field (base64) and the extension ID are derived from.
func PublicKeyDER(key *rsa.PrivateKey) ([]byte, error) {
	return x509.MarshalPKIXPublicKey(&key.PublicKey)
}

// Pack signs a ZIP archive with key and returns the CRX3 file.
func Pack(archive []byte, key *rsa.PrivateKey) ([]byte, error) {
	pub, err := PublicKeyDER(key)
	if err != nil {
		return nil, err
	}
	sum := sha256.Sum256(pub)
	signedHeaderData := appendBytesField(nil, fieldSignedDataCrxID, sum[:16])

	digest := sha256.Sum256(signedPayload(signedHeaderData, archive))
	sig, err := rsa.SignPKCS1v15(rand.Reader, key, crypto.SHA256, digest[:])
	if err != nil {
		return nil, fmt.Errorf("crx: sign: %w", err)
	}

	proof := appendBytesField(nil, fieldProofPublicKey, pub)
	proof = appendBytesField(proof, fieldProofSignature, sig)
	header := appendBytesField(nil, fieldSHA256WithRSA, proof)
	header = appendBytesField(header, fieldSignedHeaderData, signedHeaderData)

	out := make([]byte, 0, 12+len(header)+len(archive))
	out = append(out, magic...)
	out = binary.LittleEndian.AppendUint32(out, formatVersion)
	out = binary.LittleEndian.AppendUint32(out, uint32(len(header)))
	out = append(out, header...)
	return append(out, archive...), nil
}

// Package is a verified CRX3 file.
type Package struct {
	ID        string // extension ID the package is signed for
	PublicKey []byte // DER SubjectPublicKeyInfo of the key matching ID
	Archive   []byte // the ZIP payload
}

// Verify parses a CRX3 file and checks it the way Chromium does for a
// developer-signed package: every RSA proof must verify, and one of them must
// use the key whose hash is the declared crx_id.
func Verify(data []byte) (Package, error) {
	if len(data) < 12 || string(data[:4]) != magic {
		return Package{}, errors.New("crx: not a CRX file")
	}
	if v := binary.LittleEndian.Uint32(data[4:8]); v != formatVersion {
		return Package{}, fmt.Errorf("crx: unsupported CRX version %d", v)
	}
	n := binary.LittleEndian.Uint32(data[8:12])
	if n > maxHeaderSize || uint64(n) > uint64(len(data)-12) {
		return Package{}, errors.New("crx: header size out of range")
	}
	header, archive := data[12:12+n], data[12+n:]

	var proofs [][]byte
	var signedHeaderData []byte
	err := walkFields(header, func(field uint64, v []byte) {
		switch field {
		case fieldSHA256WithRSA:
			proofs = append(proofs, v)
		case fieldSignedHeaderData:
			signedHeaderData = v
		}
	})
	if err != nil {
		return Package{}, fmt.Errorf("crx: header: %w", err)
	}
	var crxID []byte
	if err := walkFields(signedHeaderData, func(field uint64, v []byte) {
		if field == fieldSignedDataCrxID {
			crxID = v
		}
	}); err != nil {
		return Package{}, fmt.Errorf("crx: signed header: %w", err)
	}
	if len(crxID) != 16 {
		return Package{}, errors.New("crx: missing crx_id")
	}
	if len(proofs) == 0 {
		return Package{}, errors.New("crx: no RSA proof")
	}

	digest := sha256.Sum256(signedPayload(signedHeaderData, archive))
	var developerKey []byte
	for _, p := range proofs {
		var pub, sig []byte
		if err := walkFields(p, func(field uint64, v []byte) {
			switch field {
			case fieldProofPublicKey:
				pub = v
			case fieldProofSignature:
				sig = v
			}
		}); err != nil {
			return Package{}, fmt.Errorf("crx: proof: %w", err)
		}
		key, err := x509.ParsePKIXPublicKey(pub)
		if err != nil {
			return Package{}, fmt.Errorf("crx: proof key: %w", err)
		}
		rk, ok := key.(*rsa.PublicKey)
		if !ok {
			return Package{}, errors.New("crx: proof key is not RSA")
		}
		if err := rsa.VerifyPKCS1v15(rk, crypto.SHA256, digest[:], sig); err != nil {
			return Package{}, errors.New("crx: signature does not verify")
		}
		if sum := sha256.Sum256(pub); bytes.Equal(sum[:16], crxID) {
			developerKey = pub
		}
	}
	if developerKey == nil {
		return Package{}, errors.New("crx: no proof from the key matching crx_id")
	}
	return Package{ID: idFromCrxID(crxID), PublicKey: developerKey, Archive: archive}, nil
}

func signedPayload(signedHeaderData, archive []byte) []byte {
	b := make([]byte, 0, len(signatureContext)+4+len(signedHeaderData)+len(archive))
	b = append(b, signatureContext...)
	b = binary.LittleEndian.AppendUint32(b, uint32(len(signedHeaderData)))
	b = append(b, signedHeaderData...)
	return append(b, archive...)
}

// appendBytesField appends a length-delimited (wire type 2) protobuf field.
func appendBytesField(b []byte, field uint64, v []byte) []byte {
	b = binary.AppendUvarint(b, field<<3|2)
	b = binary.AppendUvarint(b, uint64(len(v)))
	return append(b, v...)
}

// walkFields calls fn for every length-delimited field in a protobuf message
// and skips varint and fixed-width fields, which CRX headers do not use.
func walkFields(b []byte, fn func(field uint64, v []byte)) error {
	for len(b) > 0 {
		tag, n := binary.Uvarint(b)
		if n <= 0 {
			return errors.New("bad tag")
		}
		b = b[n:]
		switch tag & 7 {
		case 0:
			_, n := binary.Uvarint(b)
			if n <= 0 {
				return errors.New("bad varint")
			}
			b = b[n:]
		case 1:
			if len(b) < 8 {
				return errors.New("truncated fixed64")
			}
			b = b[8:]
		case 5:
			if len(b) < 4 {
				return errors.New("truncated fixed32")
			}
			b = b[4:]
		case 2:
			l, n := binary.Uvarint(b)
			if n <= 0 || l > uint64(len(b)-n) {
				return errors.New("bad length")
			}
			fn(tag>>3, b[n:n+int(l)])
			b = b[n+int(l):]
		default:
			return fmt.Errorf("unsupported wire type %d", tag&7)
		}
	}
	return nil
}
