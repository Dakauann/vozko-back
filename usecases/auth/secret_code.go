package auth_usecase

import (
	"crypto/sha256"
	"encoding/hex"
)

// hashSecretCode returns the hex SHA-256 of a short verification/reset code so it
// can be stored and compared without ever persisting the plaintext. Codes are
// low-entropy (6 digits), so security comes from binding the lookup to the
// account plus a wrong-attempt cap; the hash only stops a leaked table from
// handing out live codes.
func hashSecretCode(code string) string {
	sum := sha256.Sum256([]byte(code))
	return hex.EncodeToString(sum[:])
}
