package mcp

import "errors"

var (
	ErrWorkspaceRequired    = errors.New("mcp: workspace id required")
	ErrServerKeyRequired    = errors.New("mcp: server key required")
	ErrURLRequired          = errors.New("mcp: url required")
	ErrURLNotHTTPS          = errors.New("mcp: url must use https")
	ErrUnknownAuthMode      = errors.New("mcp: unknown auth mode")
	ErrCredentialRequired   = errors.New("mcp: credential required for auth mode")
	ErrBindingNotFound      = errors.New("mcp: binding not found")
	ErrRemoteServerNotFound = errors.New("mcp: remote server not found")
	ErrToolNotFound         = errors.New("mcp: tool not found")
	ErrToolNameMalformed    = errors.New("mcp: tool name must be kind:source.tool")
	ErrNameRequired         = errors.New("mcp: name required")
	ErrDuplicate            = errors.New("mcp: already exists")

	ErrCollectionNotFound            = errors.New("mcp: collection not found")
	ErrInvalidCollectionMemberKind   = errors.New("mcp: collection member kind must be builtin or remote")
	ErrCollectionMemberRefIDRequired = errors.New("mcp: collection member refId required")
	ErrCollectionMemberNotFound      = errors.New("mcp: collection member not found in workspace")
)
