package apikey

import (
	"crypto/sha256"
	"crypto/subtle"
	"encoding/base32"
	"encoding/base64"
	"encoding/hex"
	"errors"
	"fmt"
	"io"
	"sort"
	"strings"
	"time"
	"unicode"
	"unicode/utf8"
)

const (
	TokenPrefix           = "wsk_"
	lookupIDLength        = 12
	secretLength          = 43
	lookupBytes           = 8
	secretBytes           = 32
	maxNameRunes          = 80
	MaxActivePerWorkspace = 25
	CacheTTL              = 60 * time.Second
	TouchInterval         = time.Minute
)

var (
	ErrNameInvalid   = errors.New("apikey: the name needs 1 to 80 characters without line breaks")
	ErrScopesInvalid = errors.New("apikey: choose at least one known scope")
	ErrNotFound      = errors.New("apikey: key not found")
	ErrRevoked       = errors.New("apikey: key revoked")
	ErrLimitReached  = errors.New("apikey: the workspace reached the number of active keys")
	ErrRandom        = errors.New("apikey: the random source failed")
)

type Scope string

const (
	ScopeEmailSend Scope = "email:send"
	ScopeEmailRead Scope = "email:read"
)

type ScopeInfo struct {
	Scope       Scope
	Description string
}

type Product struct {
	Key    string
	Scopes []ScopeInfo
}

var Catalog = []Product{
	{Key: "email", Scopes: []ScopeInfo{
		{Scope: ScopeEmailSend, Description: "Enviar e-mails transacionais"},
		{Scope: ScopeEmailRead, Description: "Ler o registro de envios e os eventos de cada e-mail"},
	}},
}

func known(s Scope) bool {
	for _, p := range Catalog {
		for _, info := range p.Scopes {
			if info.Scope == s {
				return true
			}
		}
	}
	return false
}

type Key struct {
	ID          string
	WorkspaceID string
	Name        string
	LookupID    string
	SecretHash  string
	Scopes      []Scope
	CreatedBy   string
	CreatedAt   time.Time
	LastUsedAt  *time.Time
	RevokedAt   *time.Time
	RevokedBy   string
}

func (k Key) Prefix() string { return TokenPrefix + k.LookupID }

func (k Key) Usable() error {
	if k.RevokedAt != nil {
		return ErrRevoked
	}
	return nil
}

func (k Key) DueForTouch(now time.Time) bool {
	return k.LastUsedAt == nil || now.Sub(*k.LastUsedAt) >= TouchInterval
}

type Principal struct {
	WorkspaceID string
	KeyID       string
	Scopes      []Scope
}

func (p Principal) Allows(s Scope) bool {
	for _, have := range p.Scopes {
		if have == s {
			return true
		}
	}
	return false
}

type Secret struct {
	Plain    string
	LookupID string
	Hash     string
}

var lookupEncoding = base32.NewEncoding("abcdefghijklmnopqrstuvwxyz234567").WithPadding(base32.NoPadding)

func Generate(random io.Reader) (Secret, error) {
	buf := make([]byte, lookupBytes+secretBytes)
	if _, err := io.ReadFull(random, buf); err != nil {
		return Secret{}, fmt.Errorf("%w: %v", ErrRandom, err)
	}
	lookup := lookupEncoding.EncodeToString(buf[:lookupBytes])[:lookupIDLength]
	plain := TokenPrefix + lookup + "_" + base64.RawURLEncoding.EncodeToString(buf[lookupBytes:])
	return Secret{Plain: plain, LookupID: lookup, Hash: Hash(plain)}, nil
}

func Hash(token string) string {
	sum := sha256.Sum256([]byte(token))
	return hex.EncodeToString(sum[:])
}

func Matches(hash, token string) bool {
	return subtle.ConstantTimeCompare([]byte(hash), []byte(Hash(token))) == 1
}

func Parse(token string) (string, bool) {
	if len(token) != len(TokenPrefix)+lookupIDLength+1+secretLength || !strings.HasPrefix(token, TokenPrefix) {
		return "", false
	}
	rest := token[len(TokenPrefix):]
	lookup, sep, secret := rest[:lookupIDLength], rest[lookupIDLength], rest[lookupIDLength+1:]
	if sep != '_' {
		return "", false
	}
	for _, r := range lookup {
		if !(r >= 'a' && r <= 'z') && !(r >= '2' && r <= '7') {
			return "", false
		}
	}
	for _, r := range secret {
		if !(r >= 'a' && r <= 'z') && !(r >= 'A' && r <= 'Z') && !(r >= '0' && r <= '9') && r != '-' && r != '_' {
			return "", false
		}
	}
	return lookup, true
}

func ValidateName(raw string) (string, error) {
	name := strings.TrimSpace(raw)
	if name == "" || utf8.RuneCountInString(name) > maxNameRunes {
		return "", ErrNameInvalid
	}
	for _, r := range name {
		if unicode.IsControl(r) {
			return "", ErrNameInvalid
		}
	}
	return name, nil
}

func ValidateScopes(raw []string) ([]Scope, error) {
	seen := make(map[Scope]bool, len(raw))
	out := make([]Scope, 0, len(raw))
	for _, s := range raw {
		scope := Scope(s)
		if !known(scope) {
			return nil, fmt.Errorf("%w: %q", ErrScopesInvalid, s)
		}
		if !seen[scope] {
			seen[scope] = true
			out = append(out, scope)
		}
	}
	if len(out) == 0 {
		return nil, ErrScopesInvalid
	}
	sort.Slice(out, func(i, j int) bool { return out[i] < out[j] })
	return out, nil
}
