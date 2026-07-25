package mcp

import (
	"strings"
	"time"
)

type CollectionMemberKind string

const (
	CollectionMemberBuiltin CollectionMemberKind = "builtin"
	CollectionMemberRemote  CollectionMemberKind = "remote"
)

func (k CollectionMemberKind) Valid() bool {
	return k == CollectionMemberBuiltin || k == CollectionMemberRemote
}

type CollectionMember struct {
	Kind  CollectionMemberKind `json:"kind"`
	RefID string               `json:"refId"`
}

func (m *CollectionMember) Normalize() {
	m.Kind = CollectionMemberKind(strings.ToLower(strings.TrimSpace(string(m.Kind))))
	m.RefID = strings.TrimSpace(m.RefID)
}

func (m CollectionMember) Validate() error {
	if !m.Kind.Valid() {
		return ErrInvalidCollectionMemberKind
	}
	if m.RefID == "" {
		return ErrCollectionMemberRefIDRequired
	}
	return nil
}

type MCPCollection struct {
	ID          string
	WorkspaceID string
	Name        string
	Description string
	Members     []CollectionMember
	CreatedAt   time.Time
	UpdatedAt   time.Time
}

func NewMCPCollection(id, ws, name, description string) (*MCPCollection, error) {
	ws = strings.TrimSpace(ws)
	if ws == "" {
		return nil, ErrWorkspaceRequired
	}
	name = strings.TrimSpace(name)
	if name == "" {
		return nil, ErrNameRequired
	}
	now := Now()
	return &MCPCollection{
		ID:          id,
		WorkspaceID: ws,
		Name:        name,
		Description: strings.TrimSpace(description),
		Members:     []CollectionMember{},
		CreatedAt:   now,
		UpdatedAt:   now,
	}, nil
}

func (c *MCPCollection) Normalize() {
	c.Name = strings.TrimSpace(c.Name)
	c.Description = strings.TrimSpace(c.Description)
	if len(c.Members) == 0 {
		c.Members = []CollectionMember{}
		return
	}
	seen := make(map[string]struct{}, len(c.Members))
	out := make([]CollectionMember, 0, len(c.Members))
	for _, m := range c.Members {
		m.Normalize()
		if m.Validate() != nil {
			continue
		}
		key := string(m.Kind) + ":" + m.RefID
		if _, ok := seen[key]; ok {
			continue
		}
		seen[key] = struct{}{}
		out = append(out, m)
	}
	c.Members = out
}

func (c *MCPCollection) Validate() error {
	if c.WorkspaceID == "" {
		return ErrWorkspaceRequired
	}
	if c.Name == "" {
		return ErrNameRequired
	}
	for _, m := range c.Members {
		if err := m.Validate(); err != nil {
			return err
		}
	}
	return nil
}

func (c *MCPCollection) HasMember(kind CollectionMemberKind, refID string) bool {
	for _, m := range c.Members {
		if m.Kind == kind && m.RefID == refID {
			return true
		}
	}
	return false
}
