package auth_usecase

import (
	"crypto/sha256"
	"encoding/hex"
)

func hashSecretCode(code string) string {
	sum := sha256.Sum256([]byte(code))
	return hex.EncodeToString(sum[:])
}
