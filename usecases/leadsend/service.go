package leadsend_usecase

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"time"

	"vozko/domain/actor"
	"vozko/domain/balance"
	"vozko/domain/cache"
	"vozko/domain/campaign"
	"vozko/domain/customfield"
	"vozko/domain/lead"
	"vozko/domain/leadaction"
	"vozko/domain/metrics"
	uw "vozko/domain/unofficial_whatsapp"
	uwc "vozko/domain/unofficial_whatsapp_campaign"
	"vozko/domain/whatsapp/template"
	wc "vozko/domain/whatsapp_campaign"
	"vozko/domain/workspace"
	wd "vozko/domain/workspace/workspace_department"
	lead_usecase "vozko/usecases/lead"
)

const LeadChunk = 5000

var (
	errIncomplete = errors.New("lead sends: a required dependency is missing")
	ErrForbidden  = leadaction.ErrForbidden
)

type Actor = lead_usecase.Actor

type Snapshots interface {
	Snapshot(ctx context.Context, workspaceID, snapshotID, after string, limit int) ([]string, error)
	DropSnapshot(ctx context.Context, workspaceID, snapshotID string) error
}

type Leads interface {
	FindByIDs(workspaceID string, ids []string) ([]*lead.Lead, error)
}

type Definitions interface {
	ListByObject(workspaceID string, objectType customfield.ObjectType) ([]*customfield.Definition, error)
}

type Store interface {
	InRunningCampaigns(ctx context.Context, workspaceID string, leadIDs []string) (map[string]bool, error)
	Tally(ctx context.Context, channel campaign.Channel, workspaceID string, campaignIDs []string, businessPhoneID string, now time.Time) ([]campaign.PartTally, error)
	SkipBeyond(ctx context.Context, channel campaign.Channel, workspaceID, campaignID string, keep int) (int64, error)
	SkipInRunning(ctx context.Context, channel campaign.Channel, workspaceID, campaignID string) (int64, error)
	DeleteStopped(ctx context.Context, channel campaign.Channel, workspaceID, campaignID string) (bool, error)
	KeyedParts(ctx context.Context, channel campaign.Channel, workspaceID, base string) ([]campaign.KeyedPart, error)
}

type CampaignCache interface {
	Forget(campaignID string)
}

type Departments interface {
	ListDepartments(workspaceID string) ([]wd.Department, error)
}

type Templates interface {
	Get(workspaceID, templateID string) (*template.Template, error)
}

type Official struct {
	Create wc.CreateCampaignUseCase
	Access wc.CampaignAccessUseCase
	Start  wc.ReviewedStartUseCase
	Cache  CampaignCache
}

type Unofficial struct {
	Create    uwc.CreateCampaignUseCase
	Access    uwc.CampaignAccessUseCase
	Start     uwc.ReviewedStartUseCase
	Instances uwc.CampaignInstanceUseCase
	Scopes    uw.DepartmentScopeSource
}

type Deps struct {
	Permissions         workspace.PermissionChecker
	Snapshots           Snapshots
	Leads               Leads
	Contacts            lead.ContactDetails
	Names               actor.Namer
	Definitions         Definitions
	Store               Store
	Gate                cache.Gate
	Departments         Departments
	CreationDepartments wd.CreationDepartmentResolver
	Templates           Templates
	Costs               template.TemplateCostReader
	Balances            balance.BalanceReader
	Caps                balance.MonthlySendCapUsageReader
	Official            Official
	Unofficial          *Unofficial
	Metrics             metrics.LeadActionMetricsRecorder
	Now                 func() time.Time
}

type Service struct {
	deps Deps
}

func NewService(deps Deps) (*Service, error) {
	missing := map[string]bool{
		"permissions":          deps.Permissions == nil,
		"snapshots":            deps.Snapshots == nil,
		"leads":                deps.Leads == nil,
		"contacts":             deps.Contacts == nil,
		"names":                deps.Names == nil,
		"definitions":          deps.Definitions == nil,
		"store":                deps.Store == nil,
		"gate":                 deps.Gate == nil,
		"departments":          deps.Departments == nil,
		"creation departments": deps.CreationDepartments == nil,
		"templates":            deps.Templates == nil,
		"costs":                deps.Costs == nil,
		"balances":             deps.Balances == nil,
		"caps":                 deps.Caps == nil,
		"official create":      deps.Official.Create == nil,
		"official access":      deps.Official.Access == nil,
		"official start":       deps.Official.Start == nil,
		"official cache":       deps.Official.Cache == nil,
		"metrics":              deps.Metrics == nil,
	}
	if u := deps.Unofficial; u != nil {
		missing["unofficial create"] = u.Create == nil
		missing["unofficial access"] = u.Access == nil
		missing["unofficial start"] = u.Start == nil
		missing["unofficial instances"] = u.Instances == nil
		missing["unofficial scopes"] = u.Scopes == nil
	}
	for name, absent := range missing {
		if absent {
			return nil, fmt.Errorf("%w: %s", errIncomplete, name)
		}
	}
	if deps.Now == nil {
		deps.Now = time.Now
	}
	return &Service{deps: deps}, nil
}

func (s *Service) now() time.Time {
	return s.deps.Now().UTC()
}

func (s *Service) gated(ctx context.Context, fn func(context.Context) error) error {
	if s.deps.Gate == nil {
		return errIncomplete
	}
	return cache.Gated(ctx, s.deps.Gate, fn)
}

func (s *Service) authorize(a Actor, channel campaign.Channel) (leadaction.Action, error) {
	if strings.TrimSpace(a.WorkspaceID) == "" {
		return "", leadaction.ErrWorkspaceRequired
	}
	if strings.TrimSpace(a.UserID) == "" {
		return "", leadaction.ErrActorRequired
	}
	action, ok := leadaction.ActionOfChannel(channel)
	if !ok {
		return "", fmt.Errorf("%w: channel %q", leadaction.ErrUnknownAction, channel)
	}
	permissions, err := leadaction.PermissionsOf(action.Requirements(leadaction.Params{}, false))
	if err != nil {
		return "", err
	}
	if !workspace.HoldsAll(s.deps.Permissions, a.WorkspaceID, a.UserID, a.IsAdmin, permissions) {
		return "", ErrForbidden
	}
	return action, nil
}

func (s *Service) unofficial() (*Unofficial, error) {
	if s.deps.Unofficial == nil {
		return nil, leadaction.ErrUnavailable
	}
	return s.deps.Unofficial, nil
}

func (s *Service) unofficialScope(a Actor) (uw.DepartmentScope, error) {
	u, err := s.unofficial()
	if err != nil {
		return uw.DepartmentScope{}, err
	}
	scope, allowed := uw.ResolveScope(u.Scopes, a.UserID, a.WorkspaceID, a.IsAdmin)
	if !allowed {
		return uw.DepartmentScope{}, ErrForbidden
	}
	return scope, nil
}
