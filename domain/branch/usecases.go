package branch

// CreateBranchInput is the create payload. Server-trusted fields (WorkspaceID,
// Actor) are tagged json:"-" and stamped by the delivery layer, never decoded
// from the client.
type CreateBranchInput struct {
	WorkspaceID string `json:"-"`
	// TargetUserID is the workspace member (by user id) this branch is created for.
	TargetUserID string `json:"userId"`
	SIPUser      string `json:"sipUser"`
	DisplayName  string `json:"displayName,omitempty"`

	Codecs      []CodecID `json:"codecs,omitempty"`
	MaxContacts int       `json:"maxContacts,omitempty"`
	DND         bool      `json:"dnd,omitempty"`

	Actor Actor `json:"-"`
}

// UpdateBranchInput is a partial update; nil fields are left unchanged. Identity
// fields (SIPUser, MemberID) are intentionally not updatable here to keep the SIP
// credential and binding stable; rotate the secret or recreate to change them.
type UpdateBranchInput struct {
	ID string `json:"-"`

	DisplayName *string   `json:"displayName,omitempty"`
	Codecs      []CodecID `json:"codecs,omitempty"`
	MaxContacts *int      `json:"maxContacts,omitempty"`
	DND         *bool     `json:"dnd,omitempty"`

	Actor Actor `json:"-"`
}

// SecretResult carries the one-time plaintext SIP password back to the caller.
// The plaintext is never persisted; only its HA1 is stored. Callers must surface
// it to the operator once and discard it.
type SecretResult struct {
	Branch *Branch `json:"branch"`
	// Secret is the plaintext SIP password to type into the phone, returned once.
	Secret string `json:"secret"`
	// Realm and SIPUser are echoed so the operator has the full credential set.
	Realm   string `json:"realm"`
	SIPUser string `json:"sipUser"`
}

type CreateUseCase interface {
	// Execute creates the branch and returns the one-time secret to configure the
	// phone with.
	Execute(input CreateBranchInput) (*SecretResult, error)
}

type UpdateUseCase interface {
	Execute(input UpdateBranchInput) (*Branch, error)
}

type GetUseCase interface {
	Execute(id string) (*Branch, error)
}

type ListByWorkspaceUseCase interface {
	Execute(workspaceID string, page, pageSize int) ([]*Branch, int64, error)
}

type ListByUserUseCase interface {
	Execute(workspaceID, userID string) ([]*Branch, error)
}

type DeleteUseCase interface {
	Execute(id string, actor Actor) error
}

type EnableUseCase interface {
	Execute(id string, enabled bool, actor Actor) (*Branch, error)
}

type RotateSecretUseCase interface {
	// Execute mints a new secret for the branch and returns the one-time plaintext.
	Execute(id string, actor Actor) (*SecretResult, error)
}
