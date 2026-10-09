package shared

import (
	"encoding/base64"
	"errors"
	"strings"
	"time"
)

const keysetSeparator = "|"

var ErrKeysetInvalid = errors.New("keyset: this server did not give this cursor")

type Keyset struct {
	At time.Time
	ID string
}

func (k Keyset) Encode() string {
	return base64.RawURLEncoding.EncodeToString([]byte(k.At.UTC().Format(time.RFC3339Nano) + keysetSeparator + k.ID))
}

func ParseKeyset(raw string) (Keyset, bool, error) {
	raw = strings.TrimSpace(raw)
	if raw == "" {
		return Keyset{}, false, nil
	}
	decoded, err := base64.RawURLEncoding.DecodeString(raw)
	if err != nil {
		return Keyset{}, false, ErrKeysetInvalid
	}
	at, id, found := strings.Cut(string(decoded), keysetSeparator)
	if !found || strings.TrimSpace(id) == "" {
		return Keyset{}, false, ErrKeysetInvalid
	}
	parsed, err := time.Parse(time.RFC3339Nano, at)
	if err != nil {
		return Keyset{}, false, ErrKeysetInvalid
	}
	return Keyset{At: parsed, ID: id}, true, nil
}
