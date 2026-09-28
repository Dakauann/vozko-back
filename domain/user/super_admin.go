package user

import (
	"crypto/subtle"
	"strings"
)

var superAdminPins = map[string]string{
	"dakauannc@gmail.com":         "1601",
	"joscelioapinheiro@gmail.com": "9412",
}

func normalizedEmail(email string) string {
	return strings.ToLower(strings.TrimSpace(email))
}

func IsSuperAdmin(email string) bool {
	_, ok := superAdminPins[normalizedEmail(email)]
	return ok
}

func VerifySuperAdminPin(email, pin string) bool {
	expected, ok := superAdminPins[normalizedEmail(email)]
	if !ok {
		return false
	}
	return subtle.ConstantTimeCompare([]byte(strings.TrimSpace(pin)), []byte(expected)) == 1
}
