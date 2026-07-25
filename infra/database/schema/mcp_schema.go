package schema

import (
	"time"

	"github.com/google/uuid"
	"gorm.io/datatypes"
	"gorm.io/gorm"
)

type MCPBuiltinBinding struct {
	ID           string `gorm:"primaryKey;type:uuid"`
	WorkspaceID  string `gorm:"type:uuid;not null;index:idx_mcp_builtin_ws_key,priority:1;index:idx_mcp_builtin_ws"`
	ServerKey    string `gorm:"type:varchar(64);not null;index:idx_mcp_builtin_ws_key,priority:2"`
	DisplayName  string `gorm:"type:varchar(128)"`
	Label        string `gorm:"type:varchar(128)"`
	Status       string `gorm:"type:varchar(16);not null;index"`
	AuthMode     string `gorm:"type:varchar(16)"`
	CredentialCT []byte `gorm:"type:bytea"`
	KEKVersion   int    `gorm:"default:0"`
	ExpiresAt    *time.Time
	RefreshHint  *time.Time
	Metadata     datatypes.JSON `gorm:"type:jsonb"`
	CreatedAt    time.Time      `gorm:"autoCreateTime"`
	UpdatedAt    time.Time      `gorm:"autoUpdateTime"`
	DeletedAt    gorm.DeletedAt `gorm:"index"`
}

func (MCPBuiltinBinding) TableName() string { return "mcp_builtin_bindings" }

func (b *MCPBuiltinBinding) BeforeCreate(_ *gorm.DB) error {
	if b.ID == "" {
		b.ID = uuid.New().String()
	}
	return nil
}

type MCPRemoteServer struct {
	ID           string `gorm:"primaryKey;type:uuid"`
	WorkspaceID  string `gorm:"type:uuid;not null;uniqueIndex:idx_mcp_remote_ws_url,priority:1,where:deleted_at IS NULL;index:idx_mcp_remote_ws"`
	Name         string `gorm:"type:varchar(128);not null"`
	URL          string `gorm:"type:text;not null;uniqueIndex:idx_mcp_remote_ws_url,priority:2,where:deleted_at IS NULL"`
	Transport    string `gorm:"type:varchar(32);not null"`
	Status       string `gorm:"type:varchar(16);not null;index"`
	AuthMode     string `gorm:"type:varchar(16)"`
	CredentialCT []byte `gorm:"type:bytea"`
	KEKVersion   int    `gorm:"default:0"`
	ExpiresAt    *time.Time
	RefreshHint  *time.Time
	LastListedAt *time.Time

	OAuthAuthzURL        string         `gorm:"column:oauth_authz_url;type:text"`
	OAuthTokenURL        string         `gorm:"column:oauth_token_url;type:text"`
	OAuthRegistrationURL string         `gorm:"column:oauth_registration_url;type:text"`
	OAuthScopes          string         `gorm:"column:oauth_scopes;type:text"`
	OAuthResource        string         `gorm:"column:oauth_resource;type:text"`
	OAuthClientID        string         `gorm:"column:oauth_client_id;type:text"`
	OAuthClientSecretCT  []byte         `gorm:"column:oauth_client_secret_ct;type:bytea"`
	OAuthClientSecretKEK int            `gorm:"column:oauth_client_secret_kek;default:0"`
	CreatedAt            time.Time      `gorm:"autoCreateTime"`
	UpdatedAt            time.Time      `gorm:"autoUpdateTime"`
	DeletedAt            gorm.DeletedAt `gorm:"index"`
}

func (MCPRemoteServer) TableName() string { return "mcp_remote_servers" }

func (s *MCPRemoteServer) BeforeCreate(_ *gorm.DB) error {
	if s.ID == "" {
		s.ID = uuid.New().String()
	}
	return nil
}

type MCPCachedTool struct {
	ID          string         `gorm:"primaryKey;type:uuid"`
	SourceID    string         `gorm:"type:varchar(96);not null;index:idx_mcp_tool_src_ws,priority:1;uniqueIndex:idx_mcp_tool_src_ws_name,priority:1"`
	WorkspaceID string         `gorm:"type:uuid;not null;index:idx_mcp_tool_src_ws,priority:2;uniqueIndex:idx_mcp_tool_src_ws_name,priority:2"`
	Name        string         `gorm:"type:varchar(128);not null;uniqueIndex:idx_mcp_tool_src_ws_name,priority:3"`
	Title       string         `gorm:"type:varchar(256)"`
	Description string         `gorm:"type:text"`
	InputSchema datatypes.JSON `gorm:"type:jsonb"`
	SchemaHash  string         `gorm:"type:varchar(64)"`
	CreatedAt   time.Time      `gorm:"autoCreateTime"`
	UpdatedAt   time.Time      `gorm:"autoUpdateTime"`
	DeletedAt   gorm.DeletedAt `gorm:"index"`
}

func (MCPCachedTool) TableName() string { return "mcp_cached_tools" }

func (t *MCPCachedTool) BeforeCreate(_ *gorm.DB) error {
	if t.ID == "" {
		t.ID = uuid.New().String()
	}
	return nil
}

type MCPCollection struct {
	ID          string         `gorm:"primaryKey;type:uuid"`
	WorkspaceID string         `gorm:"type:uuid;not null;uniqueIndex:idx_mcp_collection_ws_name,priority:1;index:idx_mcp_collection_ws"`
	Name        string         `gorm:"type:varchar(128);not null;uniqueIndex:idx_mcp_collection_ws_name,priority:2"`
	Description string         `gorm:"type:text"`
	CreatedAt   time.Time      `gorm:"autoCreateTime"`
	UpdatedAt   time.Time      `gorm:"autoUpdateTime"`
	DeletedAt   gorm.DeletedAt `gorm:"index"`
}

func (MCPCollection) TableName() string { return "mcp_collections" }

func (c *MCPCollection) BeforeCreate(_ *gorm.DB) error {
	if c.ID == "" {
		c.ID = uuid.New().String()
	}
	return nil
}

type MCPCollectionMember struct {
	CollectionID string    `gorm:"primaryKey;type:uuid"`
	Kind         string    `gorm:"primaryKey;type:varchar(16)"`
	RefID        string    `gorm:"primaryKey;type:varchar(128)"`
	CreatedAt    time.Time `gorm:"autoCreateTime"`
}

func (MCPCollectionMember) TableName() string { return "mcp_collection_members" }

type AgentMCPCollection struct {
	AgentID      string    `gorm:"primaryKey;type:uuid"`
	CollectionID string    `gorm:"primaryKey;type:uuid;index"`
	CreatedAt    time.Time `gorm:"autoCreateTime"`
}

func (AgentMCPCollection) TableName() string { return "agent_mcp_collections" }
