package mcp

import (
	"encoding/json"
	"time"
)

type Credential struct {
	Mode        AuthMode
	Cipher      []byte
	KEKVersion  int
	ExpiresAt   *time.Time
	RefreshHint *time.Time
}

func (c *Credential) ShouldRefresh(now time.Time) bool {
	if c == nil || c.Mode != AuthOAuth2 || c.RefreshHint == nil {
		return false
	}
	return !now.Before(*c.RefreshHint)
}

func (c *Credential) Expired(now time.Time) bool {
	if c == nil || c.ExpiresAt == nil {
		return false
	}
	return !now.Before(*c.ExpiresAt)
}

type OAuth2Secret struct {
	Version      int    `json:"v"`
	AccessToken  string `json:"a"`
	RefreshToken string `json:"r,omitempty"`
}

func EncodeOAuth2Secret(access, refresh string) ([]byte, error) {
	return json.Marshal(OAuth2Secret{Version: 1, AccessToken: access, RefreshToken: refresh})
}

func DecodeOAuth2Secret(plain []byte) OAuth2Secret {
	var s OAuth2Secret
	if err := json.Unmarshal(plain, &s); err == nil && s.Version > 0 {
		return s
	}
	return OAuth2Secret{Version: 0, AccessToken: string(plain)}
}
