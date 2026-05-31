// Package codes generates and normalizes short, human-shareable poll codes.
//
// Codes use the Crockford base32 alphabet, which omits the visually ambiguous
// letters I, L, O and U. Lookups are case-insensitive and forgiving: O is read
// as 0 and I/L as 1, and separators such as spaces and hyphens are ignored.
package codes

import (
	"crypto/rand"
	"errors"
	"math/big"
	"strings"
)

// alphabet is the Crockford base32 symbol set (no I, L, O, U).
const alphabet = "0123456789ABCDEFGHJKMNPQRSTVWXYZ"

// DefaultLength is the number of characters in a generated share code.
// 32^6 ≈ 1.07e9 possibilities — ample for a self-hosted instance.
const DefaultLength = 6

// ErrExhausted is returned when GenerateUnique cannot find a free code.
var ErrExhausted = errors.New("codes: exhausted attempts generating a unique code")

// Generate returns a random share code of length n. If n <= 0, DefaultLength
// is used.
func Generate(n int) (string, error) {
	if n <= 0 {
		n = DefaultLength
	}
	max := big.NewInt(int64(len(alphabet)))
	var b strings.Builder
	b.Grow(n)
	for i := 0; i < n; i++ {
		idx, err := rand.Int(rand.Reader, max)
		if err != nil {
			return "", err
		}
		b.WriteByte(alphabet[idx.Int64()])
	}
	return b.String(), nil
}

// GenerateUnique generates codes of length n until exists reports the code is
// free, or until attempts are exhausted.
func GenerateUnique(n, attempts int, exists func(code string) (bool, error)) (string, error) {
	if attempts <= 0 {
		attempts = 8
	}
	for i := 0; i < attempts; i++ {
		code, err := Generate(n)
		if err != nil {
			return "", err
		}
		taken, err := exists(code)
		if err != nil {
			return "", err
		}
		if !taken {
			return code, nil
		}
	}
	return "", ErrExhausted
}

// Normalize canonicalizes a user-entered code for lookup. It uppercases the
// input, applies Crockford's forgiving substitutions (O->0, I/L->1) and drops
// any characters outside the alphabet (e.g. spaces and hyphens). It does not
// enforce length.
func Normalize(s string) string {
	s = strings.ToUpper(strings.TrimSpace(s))
	var b strings.Builder
	b.Grow(len(s))
	for _, r := range s {
		switch r {
		case 'O':
			r = '0'
		case 'I', 'L':
			r = '1'
		}
		if strings.ContainsRune(alphabet, r) {
			b.WriteRune(r)
		}
	}
	return b.String()
}

// Valid reports whether s normalizes to a well-formed code of length n.
func Valid(s string, n int) bool {
	if n <= 0 {
		n = DefaultLength
	}
	return len(Normalize(s)) == n
}
