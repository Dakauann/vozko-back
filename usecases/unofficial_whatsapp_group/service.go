package unofficial_whatsapp_group_usecase

import (
	"context"
	"log"
	"sync"
	"time"

	"vozko/domain/ai"
	"vozko/domain/balance"
	"vozko/domain/config"
	uw "vozko/domain/unofficial_whatsapp"
	uwg "vozko/domain/unofficial_whatsapp_group"
	balance_usecase "vozko/usecases/balance"
	uwuc "vozko/usecases/unofficial_whatsapp"
)

type SeatSync interface {
	Get(ctx context.Context, workspaceID, instanceID, groupJID string, refresh bool, scope uw.DepartmentScope) (*uw.Group, error)
	InviteLink(ctx context.Context, workspaceID, instanceID, groupJID string, scope uw.DepartmentScope) (string, error)
	UpdateName(ctx context.Context, in uwuc.UpdateNameInput) (*uw.Group, error)
	UpdateDescription(ctx context.Context, in uwuc.UpdateDescriptionInput) (*uw.Group, error)
	UpdateSettings(ctx context.Context, in uwuc.UpdateSettingsInput) (*uw.Group, error)
	UpdateImage(ctx context.Context, workspaceID, instanceID, groupJID, url string, scope uw.DepartmentScope) (*uw.Group, error)
	UpdateParticipants(ctx context.Context, in uwuc.UpdateParticipantsInput) (*uw.Group, error)
	Leave(ctx context.Context, workspaceID, instanceID, groupJID string, scope uw.DepartmentScope) error
}

type LeadLinker interface {
	EnsureLeadForPhone(ctx context.Context, workspaceID, phone, name string) (string, error)
}

type Locations interface {
	Location(ctx context.Context, workspaceID, departmentID string) (*time.Location, error)
}

type Generator interface {
	Generate(ctx context.Context, input ai.GenerateInput) (*ai.GenerateOutput, error)
}

type Deps struct {
	Groups    uwg.GroupRepository
	Members   uwg.MemberRepository
	Stats     uwg.StatsReader
	Analyses  uwg.AnalysisRepository
	Runs      uwg.RunRepository
	Joins     uwg.JoinRepository
	Messages  uwg.MessageReader
	Campaigns uwg.CampaignReader
	Thread    Thread
	Contacts  ContactFinder

	Instances uw.InstanceRepository
	Servers   uw.ServerRepository
	Seats     uw.GroupRepository
	GroupAPI  uw.GroupAPI
	SeatSync  SeatSync
	Leads     LeadLinker
	Locations Locations

	AI      Generator
	Model   string
	Balance balance.CachedBalanceChecker

	Owner string
	Clock func() time.Time
}

type Service struct {
	d             Deps
	spend         balance_usecase.SpendGuard
	primaryChecks sync.Map
}

func New(d Deps) *Service {
	if d.Clock == nil {
		d.Clock = func() time.Time { return time.Now().UTC() }
	}
	if d.Owner == "" {
		d.Owner = "groups"
	}
	return &Service{d: d, spend: balance_usecase.NewRequiredSpendGuard(d.Balance, "group analysis")}
}

type Actor struct {
	WorkspaceID string
	UserID      string
	Departments uw.DepartmentScope
}

type numbers struct {
	scope    uwg.Scope
	inScope  []*uw.Instance
	byID     map[string]*uw.Instance
	everyone []*uw.Instance
}

func (n numbers) visible(instanceID string) (*uw.Instance, bool) {
	instance, ok := n.byID[instanceID]
	if !ok || !n.scope.Allows(instanceID) {
		return nil, false
	}
	return instance, true
}

func (s *Service) numbersOf(ctx context.Context, a Actor) (numbers, error) {
	if a.WorkspaceID == "" {
		return numbers{}, uwg.ErrWorkspaceRequired
	}
	all, err := s.workspaceInstances(ctx, a.WorkspaceID)
	if err != nil {
		return numbers{}, err
	}
	n := numbers{scope: uwg.ScopeOf(a.Departments, all), byID: map[string]*uw.Instance{}, everyone: all}
	for _, instance := range all {
		n.byID[instance.ID] = instance
		if n.scope.Allows(instance.ID) {
			n.inScope = append(n.inScope, instance)
		}
	}
	return n, nil
}

func (s *Service) workspaceInstances(ctx context.Context, workspaceID string) ([]*uw.Instance, error) {
	return uwuc.AllWorkspaceInstances(ctx, s.d.Instances, workspaceID)
}

func (s *Service) ref(ctx context.Context, instance *uw.Instance) (uw.InstanceRef, error) {
	server, err := s.d.Servers.FindByID(ctx, instance.ServerID)
	if err != nil {
		return uw.InstanceRef{}, err
	}
	return uw.RefFor(server, instance), nil
}

func (s *Service) location(ctx context.Context, workspaceID string) *time.Location {
	if s.d.Locations == nil {
		return config.BrazilianTimezone
	}
	loc, err := s.d.Locations.Location(ctx, workspaceID, "")
	if err != nil {
		log.Printf("[groups] could not read the timezone of workspace %s, using %s: %v", workspaceID, config.BrazilianTimezone, err)
		return config.BrazilianTimezone
	}
	if loc == nil {
		return config.BrazilianTimezone
	}
	return loc
}

func ownNumbers(instances []*uw.Instance) uwg.OwnNumbers {
	phones := make([]string, 0, len(instances))
	jids := make([]string, 0, len(instances)*2)
	for _, i := range instances {
		phones = append(phones, i.PhoneNumber)
		jids = append(jids, i.JID, i.LID)
	}
	return uwg.NewOwnNumbers(phones, jids)
}
