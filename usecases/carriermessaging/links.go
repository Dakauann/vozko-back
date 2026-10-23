package carriermessaging_usecase

import (
	"crypto/hmac"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"net/url"
	"strings"

	"vozko/domain/rcs"
	"vozko/pkg/webhookauth"
)

const (
	linkPath          = "/public/rcs/r/"
	linkSeparator     = "~"
	minLinkSecretSize = 32
)

var ErrLinkInvalid = errors.New("rcs: the tracked link is not valid")

type LinkTokens struct {
	secret  string
	baseURL string
}

func NewLinkTokens(secret, baseURL string) (*LinkTokens, error) {
	if len(secret) < minLinkSecretSize {
		return nil, errors.New("rcs: the link secret needs at least 32 characters")
	}
	parsed, err := url.Parse(strings.TrimSpace(baseURL))
	if err != nil || parsed.Scheme != "https" || parsed.Host == "" {
		return nil, errors.New("rcs: the link base must be an https address")
	}
	return &LinkTokens{secret: secret, baseURL: strings.TrimRight(parsed.String(), "/")}, nil
}

func (l *LinkTokens) TrackedURL(messageID string, key rcs.ButtonKey) (string, error) {
	if strings.TrimSpace(messageID) == "" || strings.Contains(messageID, linkSeparator) {
		return "", ErrLinkInvalid
	}
	code := strings.TrimPrefix(key.Postback(), "vz:")
	payload := messageID + linkSeparator + code
	return l.baseURL + linkPath + payload + linkSeparator + l.sign(payload), nil
}

func (l *LinkTokens) Verify(token string) (string, rcs.ButtonKey, error) {
	parts := strings.Split(token, linkSeparator)
	if len(parts) != 3 {
		return "", rcs.ButtonKey{}, ErrLinkInvalid
	}
	payload := parts[0] + linkSeparator + parts[1]
	if !webhookauth.VerifyHexHMAC(l.secret, []byte(payload), parts[2]) {
		return "", rcs.ButtonKey{}, ErrLinkInvalid
	}
	key, ok := rcs.ParsePostback("vz:" + parts[1])
	if !ok {
		return "", rcs.ButtonKey{}, ErrLinkInvalid
	}
	return parts[0], key, nil
}

func (l *LinkTokens) sign(payload string) string {
	mac := hmac.New(sha256.New, []byte(l.secret))
	mac.Write([]byte(payload))
	return hex.EncodeToString(mac.Sum(nil))
}
