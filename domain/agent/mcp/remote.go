package mcp

import (
	"net/url"
	"os"
	"strings"
	"time"
)

type RemoteMCPServer struct {
	ID           string
	WorkspaceID  string
	Name         string
	URL          string
	Transport    Transport
	Credential   *Credential
	Status       Status
	LastListedAt *time.Time
	CreatedAt    time.Time

	OAuth *RemoteOAuthConfig
}

type RemoteOAuthConfig struct {
	AuthzURL        string
	TokenURL        string
	RegistrationURL string
	Scopes          []string

	Resource string

	ClientID string

	ClientSecretCipher []byte

	ClientSecretKEK uint32
}

func NewRemoteMCPServer(id, ws, name, rawURL string, transport Transport) (*RemoteMCPServer, error) {
	if ws == "" {
		return nil, ErrWorkspaceRequired
	}
	if strings.TrimSpace(name) == "" {
		return nil, ErrNameRequired
	}
	if rawURL == "" {
		return nil, ErrURLRequired
	}
	u, err := url.Parse(rawURL)
	if err != nil {
		return nil, ErrURLRequired
	}
	if u.Scheme == "https" {

	} else if u.Scheme == "http" && isDev() && (u.Hostname() == "localhost" || u.Hostname() == "127.0.0.1") {

	} else {
		return nil, ErrURLNotHTTPS
	}
	if transport == "" {
		transport = TransportStreamableHTTP
	}
	now := Now()
	return &RemoteMCPServer{
		ID:          id,
		WorkspaceID: ws,
		Name:        name,
		URL:         rawURL,
		Transport:   transport,
		Status:      StatusPending,
		CreatedAt:   now,
	}, nil
}

func isDev() bool {
	return os.Getenv("VOZKO_ENV") == "development"
}
