package metasignedrequest

import (
	"crypto/hmac"
	"crypto/sha256"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"time"
)

var ErrInvalid = errors.New("meta signed_request: invalid")

type Payload struct {
	UserID   string
	IssuedAt time.Time
}

type rawPayload struct {
	Algorithm string          `json:"algorithm"`
	IssuedAt  int64           `json:"issued_at"`
	UserID    json.RawMessage `json:"user_id"`
}

func Parse(signedRequest, appSecret string) (*Payload, error) {
	if strings.TrimSpace(appSecret) == "" {
		return nil, fmt.Errorf("%w: no app secret configured", ErrInvalid)
	}
	encodedSig, encodedPayload, ok := strings.Cut(strings.TrimSpace(signedRequest), ".")
	if !ok || encodedSig == "" || encodedPayload == "" {
		return nil, fmt.Errorf("%w: malformed", ErrInvalid)
	}
	sig, err := decode(encodedSig)
	if err != nil {
		return nil, fmt.Errorf("%w: signature encoding", ErrInvalid)
	}
	mac := hmac.New(sha256.New, []byte(appSecret))
	mac.Write([]byte(encodedPayload))
	if !hmac.Equal(sig, mac.Sum(nil)) {
		return nil, fmt.Errorf("%w: signature mismatch", ErrInvalid)
	}
	body, err := decode(encodedPayload)
	if err != nil {
		return nil, fmt.Errorf("%w: payload encoding", ErrInvalid)
	}
	var p rawPayload
	if err := json.Unmarshal(body, &p); err != nil {
		return nil, fmt.Errorf("%w: payload json", ErrInvalid)
	}
	if !strings.EqualFold(p.Algorithm, "HMAC-SHA256") {
		return nil, fmt.Errorf("%w: unsupported algorithm %q", ErrInvalid, p.Algorithm)
	}
	userID := idText(p.UserID)
	if userID == "" {
		return nil, fmt.Errorf("%w: missing user_id", ErrInvalid)
	}
	out := &Payload{UserID: userID}
	if p.IssuedAt > 0 {
		out.IssuedAt = time.Unix(p.IssuedAt, 0).UTC()
	}
	return out, nil
}

func idText(raw json.RawMessage) string {
	var text string
	if err := json.Unmarshal(raw, &text); err == nil {
		return strings.TrimSpace(text)
	}
	var number json.Number
	if err := json.Unmarshal(raw, &number); err == nil {
		return number.String()
	}
	return ""
}

func decode(s string) ([]byte, error) {

	return base64.RawURLEncoding.DecodeString(strings.TrimRight(s, "="))
}
