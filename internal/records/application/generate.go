package application

import (
	"crypto/rand"
	"math/big"

	shared "github.com/m7medVision/albear/internal/shared/domain"
)

// GenerateOptions controls password generation.
type GenerateOptions struct {
	Length  int
	Upper   bool
	Lower   bool
	Digits  bool
	Symbols bool
}

var DefaultGenerateOptions = GenerateOptions{Length: 24, Upper: true, Lower: true, Digits: true, Symbols: true}

const (
	setUpper   = "ABCDEFGHJKLMNPQRSTUVWXYZ"
	setLower   = "abcdefghijkmnopqrstuvwxyz"
	setDigits  = "23456789"
	setSymbols = "!@#$%^&*-_=+?"
)

// GeneratePassword produces a random password with at least one character
// from every enabled class. Ambiguous glyphs (0/O, 1/l/I) are excluded.
func GeneratePassword(opts GenerateOptions) ([]byte, error) {
	if opts.Length < 8 || opts.Length > 256 {
		return nil, shared.ErrValidation
	}
	var classes []string
	if opts.Upper {
		classes = append(classes, setUpper)
	}
	if opts.Lower {
		classes = append(classes, setLower)
	}
	if opts.Digits {
		classes = append(classes, setDigits)
	}
	if opts.Symbols {
		classes = append(classes, setSymbols)
	}
	if len(classes) == 0 {
		return nil, shared.ErrValidation
	}
	if opts.Length < len(classes) {
		return nil, shared.ErrValidation
	}

	all := ""
	for _, c := range classes {
		all += c
	}
	out := make([]byte, opts.Length)
	// One guaranteed pick per class, the rest from the full alphabet.
	for i := range out {
		src := all
		if i < len(classes) {
			src = classes[i]
		}
		n, err := rand.Int(rand.Reader, big.NewInt(int64(len(src))))
		if err != nil {
			return nil, err
		}
		out[i] = src[n.Int64()]
	}
	// Shuffle so the guaranteed picks are not positionally predictable.
	for i := len(out) - 1; i > 0; i-- {
		j, err := rand.Int(rand.Reader, big.NewInt(int64(i+1)))
		if err != nil {
			return nil, err
		}
		out[i], out[j.Int64()] = out[j.Int64()], out[i]
	}
	return out, nil
}
