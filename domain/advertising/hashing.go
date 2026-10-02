package advertising

import (
	"crypto/sha256"
	"encoding/hex"
	"strings"
	"unicode"
)

type MatchKey string

const (
	MatchEmail      MatchKey = "EMAIL"
	MatchPhone      MatchKey = "PHONE"
	MatchFirstName  MatchKey = "FN"
	MatchLastName   MatchKey = "LN"
	MatchCity       MatchKey = "CT"
	MatchState      MatchKey = "ST"
	MatchZip        MatchKey = "ZIP"
	MatchCountry    MatchKey = "COUNTRY"
	MatchExternalID MatchKey = "EXTERN_ID"
	MatchGender     MatchKey = "GEN"
	MatchBirthYear  MatchKey = "DOBY"
)

func AllMatchKeys() []MatchKey {
	return []MatchKey{MatchEmail, MatchPhone, MatchFirstName, MatchLastName, MatchCity, MatchState, MatchZip, MatchCountry, MatchExternalID, MatchGender, MatchBirthYear}
}

func (k MatchKey) Valid() bool {
	for _, known := range AllMatchKeys() {
		if known == k {
			return true
		}
	}
	return false
}

func lettersOnly(raw string) string {
	var b strings.Builder
	for _, r := range strings.ToLower(strings.TrimSpace(raw)) {
		if unicode.IsLetter(r) || unicode.IsDigit(r) {
			b.WriteRune(r)
		}
	}
	return b.String()
}

func NormalizePhone(raw, defaultCountryCode string) string {
	digits := strings.TrimLeft(DigitsOnly(raw), "0")
	if digits == "" {
		return ""
	}
	if code := DigitsOnly(defaultCountryCode); code != "" && !strings.HasPrefix(digits, code) && len(digits) >= 10 && len(digits) <= 11 {
		digits = code + digits
	}
	if len(digits) < 10 || len(digits) > 15 {
		return ""
	}
	return digits
}

func NormalizeEmail(raw string) string {
	email := strings.ToLower(strings.TrimSpace(raw))
	at := strings.Index(email, "@")
	if at <= 0 || at == len(email)-1 || !strings.Contains(email[at:], ".") || strings.ContainsAny(email, " \t") {
		return ""
	}
	return email
}

func NormalizeMatch(key MatchKey, raw, defaultCountryCode string) string {
	switch key {
	case MatchEmail:
		return NormalizeEmail(raw)
	case MatchPhone:
		return NormalizePhone(raw, defaultCountryCode)
	case MatchFirstName, MatchLastName, MatchCity, MatchState:
		return lettersOnly(raw)
	case MatchZip:
		return strings.ToLower(strings.NewReplacer(" ", "", "-", "").Replace(strings.TrimSpace(raw)))
	case MatchCountry:
		c := lettersOnly(raw)
		if len(c) != 2 {
			return ""
		}
		return c
	case MatchGender:
		g := lettersOnly(raw)
		if g == "" {
			return ""
		}
		switch g[0] {
		case 'm':
			return "m"
		case 'f':
			return "f"
		}
		return ""
	case MatchBirthYear:
		y := DigitsOnly(raw)
		if len(y) != 4 {
			return ""
		}
		return y
	case MatchExternalID:
		return strings.TrimSpace(raw)
	}
	return ""
}

func SHA256Hex(value string) string {
	sum := sha256.Sum256([]byte(value))
	return hex.EncodeToString(sum[:])
}

func HashMatch(key MatchKey, raw, defaultCountryCode string) string {
	normalized := NormalizeMatch(key, raw, defaultCountryCode)
	if normalized == "" {
		return ""
	}
	return SHA256Hex(normalized)
}

type Customer map[MatchKey]string

type HashedCustomers struct {
	Keys    []MatchKey
	Rows    [][]string
	Skipped int
}

func HashCustomers(keys []MatchKey, customers []Customer, defaultCountryCode string) HashedCustomers {
	out := HashedCustomers{Keys: keys}
	for _, c := range customers {
		row := make([]string, len(keys))
		matched := false
		for i, k := range keys {
			row[i] = HashMatch(k, c[k], defaultCountryCode)
			matched = matched || (row[i] != "" && k != MatchCountry && k != MatchGender && k != MatchBirthYear)
		}
		if !matched {
			out.Skipped++
			continue
		}
		out.Rows = append(out.Rows, row)
	}
	return out
}
