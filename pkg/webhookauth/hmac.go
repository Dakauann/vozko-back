package webhookauth

import (
	"crypto/hmac"
	"crypto/rand"
	"crypto/sha256"
	"crypto/subtle"
	"encoding/hex"
	"strings"
)

func ConstantTimeEqual(a, b string) bool {
	return subtle.ConstantTimeCompare([]byte(a), []byte(b)) == 1
}

func VerifyHexHMAC(secret string, body []byte, hexSig string) bool {
	if secret == "" || hexSig == "" {
		return false
	}
	expected, err := hex.DecodeString(strings.TrimSpace(hexSig))
	if err != nil {
		return false
	}
	mac := hmac.New(sha256.New, []byte(secret))
	mac.Write(body)
	return hmac.Equal(mac.Sum(nil), expected)
}

func VerifyPrefixedHMAC(secret string, body []byte, sigHeader string) bool {
	if sigHeader == "" {
		return false
	}
	parts := strings.SplitN(sigHeader, "=", 2)
	if len(parts) != 2 || parts[0] != "sha256" {
		return false
	}
	return VerifyHexHMAC(secret, body, parts[1])
}

func RandomToken(nBytes int) (string, error) {
	b := make([]byte, nBytes)
	if _, err := rand.Read(b); err != nil {
		return "", err
	}
	return hex.EncodeToString(b), nil
}
