package emailtoken

import (
	"crypto/hmac"
	"crypto/sha256"
	"encoding/base32"
	"encoding/hex"
	"errors"
	"strings"

	"vozko/domain/emailaddress"
)

var (
	ErrWeakSecret   = errors.New("emailtoken: secret must have at least 32 bytes")
	ErrInvalidToken = errors.New("emailtoken: invalid token")
	ErrNotTagged    = errors.New("emailtoken: address carries no token")
	ErrInvalidID    = errors.New("emailtoken: id must be a uuid")
	ErrPurpose      = errors.New("emailtoken: unknown purpose")
)

type Purpose string

const (
	PurposeReply       Purpose = "reply"
	PurposeUnsubscribe Purpose = "unsubscribe"
)

const (
	minSecretLength = 32
	idLength        = 16
	macLength       = 10
)

var encoding = base32.NewEncoding("abcdefghijklmnopqrstuvwxyz234567").WithPadding(base32.NoPadding)

type Codec struct {
	key []byte
}

func NewCodec(secret string) (*Codec, error) {
	if len(strings.TrimSpace(secret)) < minSecretLength {
		return nil, ErrWeakSecret
	}
	return &Codec{key: []byte(secret)}, nil
}

func (p Purpose) valid() bool {
	return p == PurposeReply || p == PurposeUnsubscribe
}

func (c *Codec) Seal(purpose Purpose, uuid string) (string, error) {
	if !purpose.valid() {
		return "", ErrPurpose
	}
	raw, err := uuidBytes(uuid)
	if err != nil {
		return "", err
	}
	payload := append(raw, c.mac(purpose, raw)...)
	return encoding.EncodeToString(payload), nil
}

func (c *Codec) Open(purpose Purpose, token string) (string, error) {
	if !purpose.valid() {
		return "", ErrPurpose
	}
	payload, err := encoding.DecodeString(strings.ToLower(strings.TrimSpace(token)))
	if err != nil || len(payload) != idLength+macLength {
		return "", ErrInvalidToken
	}
	raw, mac := payload[:idLength], payload[idLength:]
	if !hmac.Equal(mac, c.mac(purpose, raw)) {
		return "", ErrInvalidToken
	}
	return formatUUID(raw), nil
}

func (c *Codec) TaggedAddress(purpose Purpose, uuid, domain string) (emailaddress.Address, error) {
	token, err := c.Seal(purpose, uuid)
	if err != nil {
		return "", err
	}
	return emailaddress.Normalize(string(purpose) + "+" + token + "@" + domain)
}

func (c *Codec) OpenAddress(address emailaddress.Address) (Purpose, string, error) {
	tag, token, ok := strings.Cut(address.LocalPart(), "+")
	if !ok {
		return "", "", ErrNotTagged
	}
	purpose := Purpose(tag)
	if !purpose.valid() {
		return "", "", ErrNotTagged
	}
	id, err := c.Open(purpose, token)
	if err != nil {
		return "", "", err
	}
	return purpose, id, nil
}

func (c *Codec) mac(purpose Purpose, raw []byte) []byte {
	h := hmac.New(sha256.New, c.key)
	h.Write([]byte(purpose))
	h.Write([]byte{0})
	h.Write(raw)
	return h.Sum(nil)[:macLength]
}

func uuidBytes(uuid string) ([]byte, error) {
	compact := strings.ReplaceAll(strings.ToLower(strings.TrimSpace(uuid)), "-", "")
	if len(compact) != idLength*2 {
		return nil, ErrInvalidID
	}
	raw, err := hex.DecodeString(compact)
	if err != nil {
		return nil, ErrInvalidID
	}
	return raw, nil
}

func formatUUID(raw []byte) string {
	h := hex.EncodeToString(raw)
	return h[0:8] + "-" + h[8:12] + "-" + h[12:16] + "-" + h[16:20] + "-" + h[20:32]
}
