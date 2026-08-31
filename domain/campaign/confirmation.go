package campaign

import (
	"crypto/rand"
	"math/big"
)

// ConfirmationCodeLength is how many digits a destructive action asks for.
const ConfirmationCodeLength = 6

// NewConfirmationCode returns the code an operator has to type back before a
// campaign reset or a history wipe goes through.
//
// crypto/rand rather than math/rand, and that is not superstition: the code is
// the only thing standing between a misclick and deleting a workspace's
// conversation history. A predictable code is a code a stray double-submit can
// satisfy.
//
// Digits only, because it is read off one screen and typed into another.
func NewConfirmationCode() (string, error) {
	const digits = "0123456789"
	code := make([]byte, ConfirmationCodeLength)
	for i := range code {
		n, err := rand.Int(rand.Reader, big.NewInt(int64(len(digits))))
		if err != nil {
			return "", err
		}
		code[i] = digits[n.Int64()]
	}
	return string(code), nil
}
