package shortlink

import (
	"crypto/rand"
	"io"
	"math/big"
	"strings"
)

const base62Alphabet = "0123456789ABCDEFGHIJKLMNOPQRSTUVWXYZabcdefghijklmnopqrstuvwxyz"

const (
	MinCodeLength     = 4
	MaxCodeLength     = 32
	DefaultCodeLength = 7
)

var randReader io.Reader = rand.Reader

var reservedCodes = map[string]struct{}{
	"r": {}, "api": {}, "admin": {}, "dashboard": {}, "auth": {}, "login": {},
	"logout": {}, "register": {}, "health": {}, "metrics": {}, "swagger": {},
	"assets": {}, "static": {}, "public": {}, "app": {}, "www": {}, "webhooks": {},
}

func ClampCodeLength(length int) int {
	if length < MinCodeLength {
		return DefaultCodeLength
	}
	if length > MaxCodeLength {
		return MaxCodeLength
	}
	return length
}

func GenerateCode(length int) (string, error) {
	length = ClampCodeLength(length)
	max := big.NewInt(int64(len(base62Alphabet)))

	var b strings.Builder
	b.Grow(length)
	for range length {
		n, err := rand.Int(randReader, max)
		if err != nil {
			return "", err
		}
		b.WriteByte(base62Alphabet[n.Int64()])
	}
	return b.String(), nil
}

func NormalizeCode(code string) string {
	return strings.TrimSpace(code)
}

func ShortURL(baseURL, code string) string {
	return strings.TrimRight(baseURL, "/") + "/" + code
}

func IsReservedCode(code string) bool {
	_, ok := reservedCodes[strings.ToLower(strings.TrimSpace(code))]
	return ok
}

func isCodeRune(r rune) bool {
	return (r >= '0' && r <= '9') ||
		(r >= 'A' && r <= 'Z') ||
		(r >= 'a' && r <= 'z') ||
		r == '-' || r == '_'
}

func ValidateCustomAlias(alias string) error {
	alias = strings.TrimSpace(alias)
	if alias == "" {
		return ErrCodeRequired
	}
	if len(alias) < MinCodeLength || len(alias) > MaxCodeLength {
		return ErrInvalidAliasLength
	}
	for _, r := range alias {
		if !isCodeRune(r) {
			return ErrInvalidAliasChar
		}
	}
	if IsReservedCode(alias) {
		return ErrReservedAlias
	}
	return nil
}
