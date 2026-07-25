package mcp

import "time"

type BuiltinAuthSpec struct {
	Mode        AuthMode
	DisplayName string

	AuthzURL        string
	TokenURL        string
	Scopes          []string
	UsePKCE         bool
	ClientIDEnv     string
	ClientSecretEnv string

	HeaderName string
	Prefix     string
}

type BuiltinDescriptor struct {
	Key         string
	DisplayName string
	Description string
	AuthSpec    BuiltinAuthSpec

	Builder func(cred *Credential) ToolSource
}

type BuiltinBinding struct {
	ID          string
	WorkspaceID string
	ServerKey   string
	DisplayName string

	Label      string
	Status     Status
	Credential *Credential
	Metadata   map[string]any
	CreatedAt  time.Time
	UpdatedAt  time.Time
}

func NewBuiltinBinding(id, ws, key, displayName, label string) (*BuiltinBinding, error) {
	if ws == "" {
		return nil, ErrWorkspaceRequired
	}
	if key == "" {
		return nil, ErrServerKeyRequired
	}
	now := Now()
	return &BuiltinBinding{
		ID:          id,
		WorkspaceID: ws,
		ServerKey:   key,
		DisplayName: displayName,
		Label:       label,
		Status:      StatusPending,
		CreatedAt:   now,
		UpdatedAt:   now,
	}, nil
}
