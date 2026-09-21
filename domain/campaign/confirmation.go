package campaign

import (
	"crypto/rand"
	"math/big"
)

const ConfirmationCodeLength = 6

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
