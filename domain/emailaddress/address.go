package emailaddress

import (
	"errors"
	"strings"
)

var (
	ErrEmpty         = errors.New("emailaddress: empty")
	ErrInvalid       = errors.New("emailaddress: invalid")
	ErrInvalidDomain = errors.New("emailaddress: invalid domain")
)

const (
	maxAddressLength   = 254
	maxLocalPartLength = 64
	maxDomainLength    = 253
	maxLabelLength     = 63
)

type Address string

func Normalize(raw string) (Address, error) {
	trimmed := strings.ToLower(strings.TrimSpace(raw))
	if trimmed == "" {
		return "", ErrEmpty
	}
	if len(trimmed) > maxAddressLength {
		return "", ErrInvalid
	}
	at := strings.IndexByte(trimmed, '@')
	if at <= 0 || strings.Count(trimmed, "@") != 1 {
		return "", ErrInvalid
	}
	local, domain := trimmed[:at], trimmed[at+1:]
	if !validLocalPart(local) {
		return "", ErrInvalid
	}
	if !validDomain(domain) {
		return "", ErrInvalid
	}
	return Address(trimmed), nil
}

func NormalizeDomain(raw string) (string, error) {
	domain := strings.TrimSuffix(strings.ToLower(strings.TrimSpace(raw)), ".")
	if !validDomain(domain) {
		return "", ErrInvalidDomain
	}
	return domain, nil
}

func (a Address) LocalPart() string {
	at := strings.LastIndexByte(string(a), '@')
	if at < 0 {
		return ""
	}
	return string(a)[:at]
}

func (a Address) Domain() string {
	at := strings.LastIndexByte(string(a), '@')
	if at < 0 {
		return ""
	}
	return string(a)[at+1:]
}

func (a Address) String() string {
	return string(a)
}

func validLocalPart(local string) bool {
	if local == "" || len(local) > maxLocalPartLength {
		return false
	}
	if strings.HasPrefix(local, ".") || strings.HasSuffix(local, ".") || strings.Contains(local, "..") {
		return false
	}
	for _, r := range local {
		if !localPartRune(r) {
			return false
		}
	}
	return true
}

func localPartRune(r rune) bool {
	switch {
	case r >= 'a' && r <= 'z', r >= '0' && r <= '9':
		return true
	}
	return strings.ContainsRune(".!#$%&'*+/=?^_`{|}~-", r)
}

func validDomain(domain string) bool {
	if domain == "" || len(domain) > maxDomainLength {
		return false
	}
	labels := strings.Split(domain, ".")
	if len(labels) < 2 {
		return false
	}
	for _, label := range labels {
		if !validLabel(label) {
			return false
		}
	}
	return !allDigits(labels[len(labels)-1])
}

func validLabel(label string) bool {
	if label == "" || len(label) > maxLabelLength {
		return false
	}
	if strings.HasPrefix(label, "-") || strings.HasSuffix(label, "-") {
		return false
	}
	for _, r := range label {
		if !(r >= 'a' && r <= 'z') && !(r >= '0' && r <= '9') && r != '-' {
			return false
		}
	}
	return true
}

func allDigits(s string) bool {
	for _, r := range s {
		if r < '0' || r > '9' {
			return false
		}
	}
	return s != ""
}
