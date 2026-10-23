package choicestats

import (
	"crypto/aes"
	"crypto/cipher"
	"crypto/hmac"
	"crypto/sha256"
	"encoding/base64"
	"errors"
	"strconv"
	"strings"

	"vozko/domain/shortlink"
)

const (
	TemplateButtonOptionPrefix = "template_button"
	TemplateURLOptionPrefix    = "template_url"

	MetaURLSuffixVariable = "{{1}}"
	MaxClickTokenLength   = 512
	maxTemplateButtons    = 10
)

func TemplateButtonOptionID(index int) string {
	return TemplateButtonOptionPrefix + ":" + strconv.Itoa(index)
}

func URLOptionID(index int) string {
	return TemplateURLOptionPrefix + ":" + strconv.Itoa(index)
}

func TrackedButtonURL(base, code string) string {
	return shortlink.ShortURL(base, code) + "/" + MetaURLSuffixVariable
}

func TrackedLinkCode(base, buttonURL string) (string, bool) {
	base = strings.TrimRight(strings.TrimSpace(base), "/")
	if base == "" {
		return "", false
	}
	rest, ok := strings.CutPrefix(strings.TrimSpace(buttonURL), base+"/")
	if !ok {
		return "", false
	}
	code, ok := strings.CutSuffix(rest, "/"+MetaURLSuffixVariable)
	if !ok || !shortlink.IsWellFormedCode(code) {
		return "", false
	}
	return code, true
}

var (
	ErrClickTokenSecretMissing = errors.New("choice stats: click tokens need the server signing secret")
	ErrClickTokenInvalid       = errors.New("choice stats: a click token carries a button index from 0 to 9 and a prompt key")
)

const clickTagSize = 12

var clickTokenEncoding = base64.RawURLEncoding.Strict()

type ClickTokens struct {
	mac   []byte
	block cipher.Block
}

func NewClickTokens(secret string) (*ClickTokens, error) {
	root := strings.TrimSpace(secret)
	if root == "" {
		return nil, ErrClickTokenSecretMissing
	}
	block, err := aes.NewCipher(deriveClickKey(root, "vozko/choicestats/click-token/enc/v1"))
	if err != nil {
		return nil, err
	}
	return &ClickTokens{mac: deriveClickKey(root, "vozko/choicestats/click-token/mac/v1"), block: block}, nil
}

func deriveClickKey(root, label string) []byte {
	mac := hmac.New(sha256.New, []byte(root))
	mac.Write([]byte(label))
	return mac.Sum(nil)
}

func (t *ClickTokens) Issue(buttonIndex int, promptKey string) (string, error) {
	if t == nil || t.block == nil {
		return "", ErrClickTokenSecretMissing
	}
	if buttonIndex < 0 || buttonIndex >= maxTemplateButtons || promptKey == "" {
		return "", ErrClickTokenInvalid
	}
	plain := append([]byte{byte(buttonIndex)}, promptKey...)
	if clickTokenEncoding.EncodedLen(clickTagSize+len(plain)) > MaxClickTokenLength {
		return "", ErrClickTokenInvalid
	}
	tag := t.tag(plain)
	out := make([]byte, clickTagSize+len(plain))
	copy(out, tag)
	t.stream(tag).XORKeyStream(out[clickTagSize:], plain)
	return clickTokenEncoding.EncodeToString(out), nil
}

func (t *ClickTokens) Verify(token string) (int, string, bool) {
	if t == nil || t.block == nil || token == "" || len(token) > MaxClickTokenLength {
		return 0, "", false
	}
	raw, err := clickTokenEncoding.DecodeString(token)
	if err != nil || len(raw) < clickTagSize+2 {
		return 0, "", false
	}
	tag, sealed := raw[:clickTagSize], raw[clickTagSize:]
	plain := make([]byte, len(sealed))
	t.stream(tag).XORKeyStream(plain, sealed)
	if !hmac.Equal(tag, t.tag(plain)) {
		return 0, "", false
	}
	index := int(plain[0])
	if index >= maxTemplateButtons {
		return 0, "", false
	}
	return index, string(plain[1:]), true
}

func (t *ClickTokens) tag(plain []byte) []byte {
	mac := hmac.New(sha256.New, t.mac)
	mac.Write(plain)
	return mac.Sum(nil)[:clickTagSize]
}

func (t *ClickTokens) stream(tag []byte) cipher.Stream {
	iv := make([]byte, aes.BlockSize)
	copy(iv, tag)
	return cipher.NewCTR(t.block, iv)
}

type TrackedLink struct {
	ShortLinkID string `json:"shortLinkId" example:"6f1d2c3b-1a2b-4c5d-8e9f-0a1b2c3d4e5f"`
	Code        string `json:"code" example:"Ab3x9Kq"`
	ButtonURL   string `json:"buttonUrl" example:"https://vzk.io/r/Ab3x9Kq/{{1}}"`
	ExampleURL  string `json:"exampleUrl" example:"https://vzk.io/r/Ab3x9Kq/exemplo"`
	Destination string `json:"destination" example:"https://loja.com/promo"`
}

const (
	trackedLinkExampleSuffix = "exemplo"
	UnattributedClickSuffix  = "s"
)

func NewTrackedLink(base, code, shortLinkID, destination string) TrackedLink {
	return TrackedLink{
		ShortLinkID: shortLinkID,
		Code:        code,
		ButtonURL:   TrackedButtonURL(base, code),
		ExampleURL:  shortlink.ShortURL(base, code) + "/" + trackedLinkExampleSuffix,
		Destination: destination,
	}
}
