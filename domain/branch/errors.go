package branch

import "errors"

var (
	ErrBranchNotFound     = errors.New("branch not found")
	ErrBranchSIPUserTaken = errors.New("a branch with this SIP user already exists in this workspace")
	ErrBranchLimitReached = errors.New("branch limit reached for this workspace")
	ErrBranchForbidden    = errors.New("you don't have permission to modify this branch")

	ErrBranchWorkspaceRequired = errors.New("workspace is required for a branch")
	ErrBranchMemberRequired    = errors.New("branch must be bound to a workspace member")
	ErrBranchMemberNotFound    = errors.New("target member not found in this workspace")

	ErrBranchSIPUserRequired = errors.New("branch SIP user is required")
	ErrBranchSIPUserInvalid  = errors.New("branch SIP user is invalid (letters, digits, . - _ only)")

	ErrBranchDisplayNameInvalid        = errors.New("branch display name is too long or contains control characters")
	ErrBranchRealmInvalid              = errors.New("branch realm is invalid")
	ErrBranchMaxContactsInvalid        = errors.New("max contacts must be between 1 and 8")
	ErrBranchCodecInvalid              = errors.New("invalid branch codec configuration")
	ErrBranchRegistrationStatusInvalid = errors.New("invalid branch registration status")
)
