package mcp

import (
	"context"
	"errors"
	"strings"

	domainmcp "vozko/domain/agent/mcp"
)

type CollectionService struct {
	Collections domainmcp.CollectionRepository
	Bindings    domainmcp.BuiltinBindingRepository
	Remotes     domainmcp.RemoteServerRepository
	Catalog     BuiltinCatalog
	NewID       func() string
}

func NewCollectionService(
	collections domainmcp.CollectionRepository,
	bindings domainmcp.BuiltinBindingRepository,
	remotes domainmcp.RemoteServerRepository,
	catalog BuiltinCatalog,
	newID func() string,
) *CollectionService {
	return &CollectionService{
		Collections: collections,
		Bindings:    bindings,
		Remotes:     remotes,
		Catalog:     catalog,
		NewID:       newID,
	}
}

type CreateCollectionInput struct {
	WorkspaceID string
	Name        string
	Description string
	Members     []domainmcp.CollectionMember
}

func (s *CollectionService) Create(ctx context.Context, in CreateCollectionInput) (*domainmcp.MCPCollection, error) {
	c, err := domainmcp.NewMCPCollection(s.newID(), in.WorkspaceID, in.Name, in.Description)
	if err != nil {
		return nil, err
	}
	c.Members = in.Members
	c.Normalize()
	if err := c.Validate(); err != nil {
		return nil, err
	}
	if err := s.verifyMembers(ctx, in.WorkspaceID, c.Members); err != nil {
		return nil, err
	}
	if err := s.Collections.Create(ctx, c); err != nil {
		return nil, err
	}
	return c, nil
}

type UpdateCollectionInput struct {
	WorkspaceID string
	ID          string
	Name        string
	Description string
	Members     []domainmcp.CollectionMember
}

func (s *CollectionService) Update(ctx context.Context, in UpdateCollectionInput) (*domainmcp.MCPCollection, error) {
	existing, err := s.Collections.Get(ctx, in.WorkspaceID, in.ID)
	if err != nil {
		return nil, err
	}
	existing.Name = strings.TrimSpace(in.Name)
	existing.Description = strings.TrimSpace(in.Description)
	existing.Members = in.Members
	existing.Normalize()
	if err := existing.Validate(); err != nil {
		return nil, err
	}
	if err := s.verifyMembers(ctx, in.WorkspaceID, existing.Members); err != nil {
		return nil, err
	}
	if err := s.Collections.Update(ctx, existing); err != nil {
		return nil, err
	}
	return existing, nil
}

func (s *CollectionService) List(ctx context.Context, ws string) ([]*domainmcp.MCPCollection, error) {
	if strings.TrimSpace(ws) == "" {
		return nil, domainmcp.ErrWorkspaceRequired
	}
	return s.Collections.ListByWorkspace(ctx, ws)
}

func (s *CollectionService) Get(ctx context.Context, ws, id string) (*domainmcp.MCPCollection, error) {
	return s.Collections.Get(ctx, ws, id)
}

func (s *CollectionService) Delete(ctx context.Context, ws, id string) error {
	return s.Collections.Delete(ctx, ws, id)
}

func (s *CollectionService) verifyMembers(ctx context.Context, ws string, members []domainmcp.CollectionMember) error {
	for _, m := range members {
		switch m.Kind {
		case domainmcp.CollectionMemberBuiltin:
			if s.Bindings == nil {
				return domainmcp.ErrCollectionMemberNotFound
			}
			b, err := s.Bindings.GetByID(ctx, ws, m.RefID)
			if err != nil {
				if errors.Is(err, domainmcp.ErrBindingNotFound) {
					return domainmcp.ErrCollectionMemberNotFound
				}
				return err
			}
			if s.Catalog != nil {
				if _, ok := s.Catalog.Descriptor(b.ServerKey); !ok {
					return domainmcp.ErrCollectionMemberNotFound
				}
			}
		case domainmcp.CollectionMemberRemote:
			if s.Remotes == nil {
				return domainmcp.ErrCollectionMemberNotFound
			}
			if _, err := s.Remotes.Get(ctx, ws, m.RefID); err != nil {
				if errors.Is(err, domainmcp.ErrRemoteServerNotFound) {
					return domainmcp.ErrCollectionMemberNotFound
				}
				return err
			}
		default:
			return domainmcp.ErrInvalidCollectionMemberKind
		}
	}
	return nil
}

func (s *CollectionService) newID() string {
	if s.NewID != nil {
		return s.NewID()
	}
	return ""
}
