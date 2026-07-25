package mcp

import "time"

type AuthMode string

const (
	AuthNone   AuthMode = "none"
	AuthAPIKey AuthMode = "api_key"
	AuthOAuth2 AuthMode = "oauth2"
)

func (m AuthMode) Valid() bool {
	switch m {
	case AuthNone, AuthAPIKey, AuthOAuth2:
		return true
	}
	return false
}

type Status string

const (
	StatusPending      Status = "pending"
	StatusConnected    Status = "connected"
	StatusDisconnected Status = "disconnected"
	StatusRevoked      Status = "revoked"
	StatusError        Status = "error"
)

type Transport string

const (
	TransportStreamableHTTP Transport = "streamable-http"
)

type Kind string

const (
	KindBuiltin Kind = "builtin"
	KindRemote  Kind = "remote"
)

type WorkspaceID string

func (w WorkspaceID) String() string { return string(w) }

func (w WorkspaceID) Empty() bool { return string(w) == "" }

var Now = time.Now
