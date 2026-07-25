package mcp

import (
	"crypto/sha256"
	"encoding/hex"
	"time"
)

type CachedTool struct {
	SourceID    string
	WorkspaceID string
	Name        string
	Title       string
	Description string
	InputSchema []byte
	Hash        string
	RefreshedAt time.Time
}

func NewCachedTool(sourceID, ws string, t Tool) CachedTool {
	sum := sha256.Sum256(t.InputSchema)
	return CachedTool{
		SourceID:    sourceID,
		WorkspaceID: ws,
		Name:        t.Name,
		Title:       t.Title,
		Description: t.Description,
		InputSchema: append([]byte(nil), t.InputSchema...),
		Hash:        hex.EncodeToString(sum[:]),
		RefreshedAt: Now(),
	}
}
