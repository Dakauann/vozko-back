package gormmcp

import (
	"encoding/json"
	"strings"

	"gorm.io/datatypes"

	domainmcp "vozko/domain/agent/mcp"
	"vozko/infra/database/schema"
)

func bindingToRow(b *domainmcp.BuiltinBinding) schema.MCPBuiltinBinding {
	row := schema.MCPBuiltinBinding{
		ID:          b.ID,
		WorkspaceID: b.WorkspaceID,
		ServerKey:   b.ServerKey,
		DisplayName: b.DisplayName,
		Label:       b.Label,
		Status:      string(b.Status),
	}
	if b.Credential != nil {
		row.AuthMode = string(b.Credential.Mode)
		row.CredentialCT = b.Credential.Cipher
		row.KEKVersion = b.Credential.KEKVersion
		row.ExpiresAt = b.Credential.ExpiresAt
		row.RefreshHint = b.Credential.RefreshHint
	}
	if len(b.Metadata) > 0 {
		if buf, err := json.Marshal(b.Metadata); err == nil {
			row.Metadata = datatypes.JSON(buf)
		}
	}
	return row
}

func rowToBinding(r *schema.MCPBuiltinBinding) *domainmcp.BuiltinBinding {
	b := &domainmcp.BuiltinBinding{
		ID:          r.ID,
		WorkspaceID: r.WorkspaceID,
		ServerKey:   r.ServerKey,
		DisplayName: r.DisplayName,
		Label:       r.Label,
		Status:      domainmcp.Status(r.Status),
		CreatedAt:   r.CreatedAt,
		UpdatedAt:   r.UpdatedAt,
	}
	if r.AuthMode != "" {
		b.Credential = &domainmcp.Credential{
			Mode:        domainmcp.AuthMode(r.AuthMode),
			Cipher:      r.CredentialCT,
			KEKVersion:  r.KEKVersion,
			ExpiresAt:   r.ExpiresAt,
			RefreshHint: r.RefreshHint,
		}
	}
	if len(r.Metadata) > 0 {
		md := map[string]any{}
		if err := json.Unmarshal(r.Metadata, &md); err == nil {
			b.Metadata = md
		}
	}
	return b
}

func remoteToRow(s *domainmcp.RemoteMCPServer) schema.MCPRemoteServer {
	row := schema.MCPRemoteServer{
		ID:           s.ID,
		WorkspaceID:  s.WorkspaceID,
		Name:         s.Name,
		URL:          s.URL,
		Transport:    string(s.Transport),
		Status:       string(s.Status),
		LastListedAt: s.LastListedAt,
	}
	if s.Credential != nil {
		row.AuthMode = string(s.Credential.Mode)
		row.CredentialCT = s.Credential.Cipher
		row.KEKVersion = s.Credential.KEKVersion
		row.ExpiresAt = s.Credential.ExpiresAt
		row.RefreshHint = s.Credential.RefreshHint
	}
	if s.OAuth != nil {
		row.OAuthAuthzURL = s.OAuth.AuthzURL
		row.OAuthTokenURL = s.OAuth.TokenURL
		row.OAuthRegistrationURL = s.OAuth.RegistrationURL
		row.OAuthScopes = strings.Join(s.OAuth.Scopes, " ")
		row.OAuthResource = s.OAuth.Resource
		row.OAuthClientID = s.OAuth.ClientID
		row.OAuthClientSecretCT = s.OAuth.ClientSecretCipher
		row.OAuthClientSecretKEK = int(s.OAuth.ClientSecretKEK)
	}
	return row
}

func rowToRemote(r *schema.MCPRemoteServer) *domainmcp.RemoteMCPServer {
	s := &domainmcp.RemoteMCPServer{
		ID:           r.ID,
		WorkspaceID:  r.WorkspaceID,
		Name:         r.Name,
		URL:          r.URL,
		Transport:    domainmcp.Transport(r.Transport),
		Status:       domainmcp.Status(r.Status),
		LastListedAt: r.LastListedAt,
		CreatedAt:    r.CreatedAt,
	}
	if r.AuthMode != "" {
		s.Credential = &domainmcp.Credential{
			Mode:        domainmcp.AuthMode(r.AuthMode),
			Cipher:      r.CredentialCT,
			KEKVersion:  r.KEKVersion,
			ExpiresAt:   r.ExpiresAt,
			RefreshHint: r.RefreshHint,
		}
	}
	if r.OAuthAuthzURL != "" || r.OAuthTokenURL != "" || r.OAuthClientID != "" {
		var scopes []string
		if r.OAuthScopes != "" {
			scopes = strings.Fields(r.OAuthScopes)
		}
		s.OAuth = &domainmcp.RemoteOAuthConfig{
			AuthzURL:           r.OAuthAuthzURL,
			TokenURL:           r.OAuthTokenURL,
			RegistrationURL:    r.OAuthRegistrationURL,
			Scopes:             scopes,
			Resource:           r.OAuthResource,
			ClientID:           r.OAuthClientID,
			ClientSecretCipher: r.OAuthClientSecretCT,
			ClientSecretKEK:    uint32(r.OAuthClientSecretKEK),
		}
	}
	return s
}

func toolToRow(t domainmcp.CachedTool) schema.MCPCachedTool {
	return schema.MCPCachedTool{
		SourceID:    t.SourceID,
		WorkspaceID: t.WorkspaceID,
		Name:        t.Name,
		Title:       t.Title,
		Description: t.Description,
		InputSchema: datatypes.JSON(t.InputSchema),
		SchemaHash:  t.Hash,
	}
}

func rowToTool(r *schema.MCPCachedTool) domainmcp.CachedTool {
	return domainmcp.CachedTool{
		SourceID:    r.SourceID,
		WorkspaceID: r.WorkspaceID,
		Name:        r.Name,
		Title:       r.Title,
		Description: r.Description,
		InputSchema: []byte(r.InputSchema),
		Hash:        r.SchemaHash,
		RefreshedAt: r.UpdatedAt,
	}
}
