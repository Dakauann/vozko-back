package customerportfolio_usecase

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/google/uuid"

	"vozko/domain/actor"
	"vozko/domain/conversation"
	cp "vozko/domain/customerportfolio"
	ia "vozko/domain/inbox_assignment"
	"vozko/domain/lead"
	"vozko/domain/shared"
	"vozko/domain/workspace"
)

var errIncomplete = errors.New("customer portfolio: a required dependency is missing")

type Actor = conversation.Viewer

type MemberReach interface {
	CanView(callerUserID, targetUserID, workspaceID string, isPlatformAdmin bool) (bool, error)
}

type Deps struct {
	Entries     cp.Repository
	Contacts    cp.ContactResolver
	EntryLeads  lead.EntryLeads
	Permissions workspace.PermissionChecker
	Receivers   ia.PortfolioReceivers
	Access      shared.EntryAccessChecker
	Reach       MemberReach
	Names       actor.NameResolver
	Now         func() time.Time
	NewID       func() string
}

type Service struct {
	deps Deps
}

func New(deps Deps) (*Service, error) {
	missing := []struct {
		name   string
		absent bool
	}{
		{"entries", deps.Entries == nil},
		{"contacts", deps.Contacts == nil},
		{"entry leads", deps.EntryLeads == nil},
		{"permissions", deps.Permissions == nil},
		{"receivers", deps.Receivers == nil},
		{"access", deps.Access == nil},
		{"reach", deps.Reach == nil},
		{"names", deps.Names == nil},
	}
	for _, m := range missing {
		if m.absent {
			return nil, fmt.Errorf("%w: %s", errIncomplete, m.name)
		}
	}
	if deps.Now == nil {
		deps.Now = time.Now
	}
	if deps.NewID == nil {
		deps.NewID = uuid.NewString
	}
	return &Service{deps: deps}, nil
}

func (s *Service) caller(a Actor) (cp.Caller, error) {
	if strings.TrimSpace(a.WorkspaceID) == "" {
		return cp.Caller{}, cp.ErrWorkspaceRequired
	}
	holds := func(key workspace.CapabilityKey) bool {
		return workspace.HoldsCapability(s.deps.Permissions, a.WorkspaceID, a.UserID, a.IsAdmin, key)
	}
	return cp.Caller{UserID: a.UserID, Views: holds(cp.CapabilityView), Uses: holds(cp.CapabilityUse), Manages: holds(cp.CapabilityManage)}, nil
}

func (s *Service) mayActOnConversation(a Actor, ref shared.EntryRef) error {
	person := shared.Person{UserID: a.UserID, SystemAdmin: a.IsAdmin}
	if !person.MayActOn(s.deps.Access, a.WorkspaceID, ref.EntryID, string(ref.EntryType)) {
		return cp.ErrForbidden
	}
	return nil
}

func (s *Service) mayReadLeads(a Actor) error {
	if !s.deps.Permissions.HasWorkspacePermission(a.UserID, a.WorkspaceID, string(workspace.ResourceLeads), string(workspace.ActionRead), a.IsAdmin) {
		return cp.ErrForbidden
	}
	return nil
}

func (s *Service) subjectOf(ctx context.Context, workspaceID string, ref shared.EntryRef) (cp.Subject, error) {
	leadID, err := s.deps.EntryLeads.LeadOfEntry(ctx, workspaceID, ref)
	switch {
	case err == nil:
		return cp.LeadSubject(leadID), nil
	case errors.Is(err, lead.ErrLeadNotFound):
		return s.deps.Contacts.ContactOfConversation(ctx, workspaceID, ref)
	default:
		return cp.Subject{}, fmt.Errorf("lead of conversation %s: %w", ref.EntryID, err)
	}
}

func (s *Service) holding(subject cp.Subject, entry *cp.Entry) (cp.Holding, error) {
	h := cp.Holding{Subject: subject, Entry: entry}
	if entry == nil {
		return h, nil
	}
	names, err := s.deps.Names.ResolveNames(entry.HolderID)
	if err != nil {
		return cp.Holding{}, fmt.Errorf("carteira holder name: %w", err)
	}
	h.HolderName = names[entry.HolderID]
	return h, nil
}

func (s *Service) now() time.Time {
	return s.deps.Now().UTC()
}
