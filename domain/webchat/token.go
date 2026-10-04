package webchat

import (
	"crypto/hmac"
	"crypto/sha256"
	"encoding/base64"
	"strconv"
	"strings"
	"time"
)

const (
	VisitorTokenTTL     = 30 * 24 * time.Hour
	visitorTokenVersion = "v1"
	tokenClockSkew      = 2 * time.Minute
)

type Keys struct {
	VisitorToken []byte
	Challenge    []byte
	IPHash       []byte
}

func DeriveKeys(rootSecret string) (Keys, error) {
	root := strings.TrimSpace(rootSecret)
	if root == "" {
		return Keys{}, ErrSigningKeyMissing
	}
	return Keys{
		VisitorToken: derive(root, "vozko/webchat/visitor-token/v1"),
		Challenge:    derive(root, "vozko/webchat/challenge/v1"),
		IPHash:       derive(root, "vozko/webchat/ip-hash/v1"),
	}, nil
}

func derive(root, label string) []byte {
	mac := hmac.New(sha256.New, []byte(root))
	mac.Write([]byte(label))
	return mac.Sum(nil)
}

func sign(key []byte, data string) string {
	mac := hmac.New(sha256.New, key)
	mac.Write([]byte(data))
	return base64.RawURLEncoding.EncodeToString(mac.Sum(nil))
}

type VisitorGrant struct {
	WidgetID  string
	VisitorID string
	IssuedAt  time.Time
	ExpiresAt time.Time
}

func NewVisitorToken(keys Keys, grant VisitorGrant, now time.Time) (string, error) {
	if len(keys.VisitorToken) == 0 {
		return "", ErrSigningKeyMissing
	}
	if grant.WidgetID == "" || grant.VisitorID == "" || strings.Contains(grant.WidgetID+grant.VisitorID, "|") {
		return "", ErrVisitorTokenInvalid
	}
	issued := now.UTC().Truncate(time.Second)
	payload := strings.Join([]string{
		grant.WidgetID,
		grant.VisitorID,
		strconv.FormatInt(issued.Unix(), 10),
		strconv.FormatInt(issued.Add(VisitorTokenTTL).Unix(), 10),
	}, "|")
	encoded := base64.RawURLEncoding.EncodeToString([]byte(payload))
	head := visitorTokenVersion + "." + encoded
	return head + "." + sign(keys.VisitorToken, head), nil
}

func VerifyVisitorToken(keys Keys, token string, now time.Time) (VisitorGrant, error) {
	if len(keys.VisitorToken) == 0 {
		return VisitorGrant{}, ErrSigningKeyMissing
	}
	parts := strings.Split(strings.TrimSpace(token), ".")
	if len(parts) != 3 || parts[0] != visitorTokenVersion || parts[1] == "" || parts[2] == "" {
		return VisitorGrant{}, ErrVisitorTokenInvalid
	}
	head := parts[0] + "." + parts[1]
	if !hmac.Equal([]byte(parts[2]), []byte(sign(keys.VisitorToken, head))) {
		return VisitorGrant{}, ErrVisitorTokenInvalid
	}
	raw, err := base64.RawURLEncoding.DecodeString(parts[1])
	if err != nil {
		return VisitorGrant{}, ErrVisitorTokenInvalid
	}
	fields := strings.Split(string(raw), "|")
	if len(fields) != 4 || fields[0] == "" || fields[1] == "" {
		return VisitorGrant{}, ErrVisitorTokenInvalid
	}
	issued, errIssued := strconv.ParseInt(fields[2], 10, 64)
	expires, errExpires := strconv.ParseInt(fields[3], 10, 64)
	if errIssued != nil || errExpires != nil {
		return VisitorGrant{}, ErrVisitorTokenInvalid
	}
	grant := VisitorGrant{
		WidgetID:  fields[0],
		VisitorID: fields[1],
		IssuedAt:  time.Unix(issued, 0).UTC(),
		ExpiresAt: time.Unix(expires, 0).UTC(),
	}
	if grant.IssuedAt.After(now.Add(tokenClockSkew)) {
		return VisitorGrant{}, ErrVisitorTokenInvalid
	}
	if now.After(grant.ExpiresAt) {
		return VisitorGrant{}, ErrVisitorTokenExpired
	}
	return grant, nil
}

func HashIP(keys Keys, ip string) string {
	ip = strings.TrimSpace(ip)
	if ip == "" || len(keys.IPHash) == 0 {
		return ""
	}
	return sign(keys.IPHash, ip)
}

func hmacEqualString(a, b string) bool {
	return hmac.Equal([]byte(a), []byte(b))
}
