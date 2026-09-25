package readiness_usecase

import (
	"context"

	"vozko/domain/readiness"
	"vozko/domain/shared"
	uw "vozko/domain/unofficial_whatsapp"
	businessphone "vozko/domain/whatsapp/business_phone"
	tmpl "vozko/domain/whatsapp/template"
	"vozko/domain/workspace"
	"vozko/domain/workspace/workspace_addon"
)

var firstPage = shared.QueryOptions{Pagination: shared.Pagination{Page: 1, PageSize: 1}}

func allowed(access workspace.CheckAccessUseCase, person readiness.Person, resource workspace.Resource) bool {
	if person.SystemAdmin {
		return true
	}
	return access != nil && access.Execute(person.UserID, person.WorkspaceID, resource, workspace.ActionCreate) == nil
}

func addable(status readiness.Status, permitted, withinLimit bool) readiness.Status {
	switch {
	case !permitted:
		status.Blocker = readiness.BlockerNoPermission
	case !withinLimit:
		status.Blocker = readiness.BlockerAtLimit
	default:
		status.CanAdd = true
	}
	return status
}

type OfficialWhatsAppDeps struct {
	Phones       businessphone.WorkspacePhonesUseCase
	Entitlements workspace_addon.GetWorkspaceEntitlementsUseCase
	Gate         businessphone.ProvisioningGate
	Access       workspace.CheckAccessUseCase
}

type officialWhatsAppProbe struct{ deps OfficialWhatsAppDeps }

func NewOfficialWhatsAppProbe(deps OfficialWhatsAppDeps) readiness.Probe {
	return &officialWhatsAppProbe{deps: deps}
}

func (p *officialWhatsAppProbe) Capability() readiness.Capability { return readiness.OfficialWhatsApp }

func (p *officialWhatsAppProbe) Probe(_ context.Context, person readiness.Person) (readiness.Status, error) {
	count, err := phoneCount(p.deps.Phones, person.WorkspaceID)
	if err != nil {
		return readiness.Status{}, err
	}
	total, err := entitlementTotal(p.deps.Entitlements, person.WorkspaceID, workspace_addon.EntitlementWhatsAppBusinessPhones)
	if err != nil {
		return readiness.Status{}, err
	}
	canProvision, err := p.deps.Gate.CanProvisionPhone(person.WorkspaceID)
	if err != nil {
		return readiness.Status{}, err
	}
	status := readiness.Status{Capability: readiness.OfficialWhatsApp, Count: count, Usage: &readiness.Usage{Used: count, Total: total}}
	return addable(status, allowed(p.deps.Access, person, workspace.ResourceBusinessPhones), canProvision), nil
}

func phoneCount(phones businessphone.WorkspacePhonesUseCase, workspaceID string) (int, error) {
	out, err := phones.List(workspaceID, businessphone.ListInput{Options: firstPage})
	if err != nil {
		return 0, err
	}
	return int(out.TotalItems), nil
}

func entitlementTotal(entitlements workspace_addon.GetWorkspaceEntitlementsUseCase, workspaceID string, kind workspace_addon.EntitlementKind) (int, error) {
	all, err := entitlements.Execute(workspaceID)
	if err != nil {
		return 0, err
	}
	for _, e := range all {
		if e.Kind == kind {
			return e.Total, nil
		}
	}
	return 0, nil
}

type TemplatesDeps struct {
	Templates tmpl.WorkspaceTemplatesUseCase
	Phones    businessphone.WorkspacePhonesUseCase
	Access    workspace.CheckAccessUseCase
}

type templatesProbe struct{ deps TemplatesDeps }

func NewTemplatesProbe(deps TemplatesDeps) readiness.Probe { return &templatesProbe{deps: deps} }

func (p *templatesProbe) Capability() readiness.Capability { return readiness.ApprovedTemplates }

func (p *templatesProbe) Probe(_ context.Context, person readiness.Person) (readiness.Status, error) {
	out, err := p.deps.Templates.List(person.WorkspaceID, tmpl.ListInput{Status: tmpl.TemplateStatusApproved, Options: firstPage})
	if err != nil {
		return readiness.Status{}, err
	}
	phones, err := phoneCount(p.deps.Phones, person.WorkspaceID)
	if err != nil {
		return readiness.Status{}, err
	}
	status := readiness.Status{Capability: readiness.ApprovedTemplates, Count: int(out.TotalItems)}
	if phones == 0 {
		status.Blocker = readiness.BlockerNeedsOfficial
		return status, nil
	}
	return addable(status, allowed(p.deps.Access, person, workspace.ResourceWhatsAppTemplates), true), nil
}

type unofficialWhatsAppProbe struct {
	allowance uw.InstanceEntitlementReader
	access    workspace.CheckAccessUseCase
}

func NewUnofficialWhatsAppProbe(allowance uw.InstanceEntitlementReader, access workspace.CheckAccessUseCase) readiness.Probe {
	return &unofficialWhatsAppProbe{allowance: allowance, access: access}
}

func (p *unofficialWhatsAppProbe) Capability() readiness.Capability { return readiness.UnofficialWhatsApp }

func (p *unofficialWhatsAppProbe) Probe(ctx context.Context, person readiness.Person) (readiness.Status, error) {
	a, err := p.allowance.AllowanceFor(ctx, person.WorkspaceID)
	if err != nil {
		return readiness.Status{}, err
	}
	status := readiness.Status{Capability: readiness.UnofficialWhatsApp, Count: a.Used, Usage: &readiness.Usage{Used: a.Used, Total: a.Limit}}
	return addable(status, allowed(p.access, person, workspace.ResourceUnofficialWhatsAppInstances), a.CanProvision()), nil
}

type Counter func(ctx context.Context, workspaceID string) (int, error)

type countProbe struct {
	capability readiness.Capability
	resource   workspace.Resource
	count      Counter
	limit      int
	access     workspace.CheckAccessUseCase
}

func NewCountProbe(capability readiness.Capability, resource workspace.Resource, count Counter, limit int, access workspace.CheckAccessUseCase) readiness.Probe {
	return &countProbe{capability: capability, resource: resource, count: count, limit: limit, access: access}
}

func (p *countProbe) Capability() readiness.Capability { return p.capability }

func (p *countProbe) Probe(ctx context.Context, person readiness.Person) (readiness.Status, error) {
	count, err := p.count(ctx, person.WorkspaceID)
	if err != nil {
		return readiness.Status{}, err
	}
	status := readiness.Status{Capability: p.capability, Count: count}
	within := true
	if p.limit > 0 {
		status.Usage = &readiness.Usage{Used: count, Total: p.limit}
		within = count < p.limit
	}
	return addable(status, allowed(p.access, person, p.resource), within), nil
}
