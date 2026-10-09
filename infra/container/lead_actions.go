package container

import (
	"context"
	"log"
	"time"

	"vozko/domain/customfield"
	lead_domain "vozko/domain/lead"
	leadaction_repository "vozko/infra/repositories/leadaction"
	adsuc "vozko/usecases/advertising"
	lead_usecase "vozko/usecases/lead"
	leadaction_usecase "vozko/usecases/leadaction"
	leadsend_usecase "vozko/usecases/leadsend"
	report_renderers "vozko/usecases/report/renderers"
)

const (
	leadMetaBlockRequests = 20
	leadMetaBlockWindow   = time.Second
)

type leadActionBundle struct {
	service *leadaction_usecase.Service
	sends   *leadsend_usecase.Service
}

func (c *Container) leadSelection() *lead_usecase.SelectionResolver {
	if c.services.conversationAuthImpl == nil {
		log.Fatalf("[lead-selection] the authorizer must be wired before lead selections")
	}
	resolver, err := lead_usecase.NewSelectionResolver(lead_usecase.SelectionDeps{
		Reader:      c.repositories.lead,
		Snapshots:   c.repositories.lead,
		Permissions: c.services.conversationAuthImpl,
		Definitions: c.repositories.customField,
		Zones:       c.attendanceScheduleResolver(),
		Areas:       c.leadAreas(),
	})
	if err != nil {
		log.Fatalf("[lead-selection] %v", err)
	}
	return resolver
}

func (c *Container) leadActions() *leadaction_usecase.Service {
	if c.leadActionBundle != nil {
		return c.leadActionBundle.service
	}
	if c.services.conversationAuthImpl == nil || c.useCases.memberVisibility == nil || c.adsManager().Audience == nil || c.services.metrics == nil {
		log.Fatalf("[lead-actions] the authorizer, member visibility, Meta audiences and metrics must be wired before lead actions")
	}
	deps := c.leadCommandDeps()
	reach, err := lead_usecase.NewOwnerReach(deps.Owners, deps.Visibility)
	if err != nil {
		log.Fatalf("[lead-actions] %v", err)
	}
	blocker, err := lead_usecase.NewWhatsAppBlocker(c.repositories.businessPhone, c.repositories.workspacePhoneAccess, c.services.businessPhoneMetaAPI)
	if err != nil {
		log.Fatalf("[lead-actions] %v", err)
	}
	sends := c.buildLeadSends()
	service, err := leadaction_usecase.NewService(leadaction_usecase.Deps{
		Runs:        leadaction_repository.NewStore(c.db),
		Writer:      c.repositories.lead,
		Notifier:    c.leadNotifier(),
		Selections:  c.leadSelection(),
		Permissions: c.services.conversationAuthImpl,
		Definitions: c.repositories.customField,
		Owners:      reach,
		MetaPhones:  blocker,
		MetaLimiter: c.services.rateLimiterFactory("lead-meta-block", leadMetaBlockRequests, leadMetaBlockWindow),
		State:       c.redisProvider.SharedState(),
		Gate:        c.sharedAnalyticsGate(),
		Reports:     c.services.reportService,
		Audiences:   c.ads.Audience,
		CallLists:   c.callLists(),
		Sends:       sends,
		Metrics:     c.services.metrics,
	})
	if err != nil {
		log.Fatalf("[lead-actions] %v", err)
	}
	c.leadActionBundle = &leadActionBundle{service: service, sends: sends}
	return service
}

func (c *Container) leadSends() *leadsend_usecase.Service {
	c.leadActions()
	return c.leadActionBundle.sends
}

type leadExportSource struct {
	*lead_usecase.SelectionResolver
	leads       lead_domain.Repository
	contacts    lead_domain.ContactDetails
	definitions lead_usecase.DefinitionSource
	names       func(ids ...string) map[string]string
}

func (s leadExportSource) FindByIDs(workspaceID string, ids []string) ([]*lead_domain.Lead, error) {
	return s.leads.FindByIDs(workspaceID, ids)
}

func (s leadExportSource) AttachContactDetails(ctx context.Context, workspaceID string, leads []*lead_domain.Lead) error {
	return s.contacts.AttachContactDetails(ctx, workspaceID, leads)
}

func (s leadExportSource) ListByObject(workspaceID string, objectType customfield.ObjectType) ([]*customfield.Definition, error) {
	return s.definitions.ListByObject(workspaceID, objectType)
}

func (s leadExportSource) Names(ids ...string) map[string]string {
	return s.names(ids...)
}

func (c *Container) leadExportRenderer() *report_renderers.LeadsRenderer {
	return report_renderers.NewLeadsRenderer(leadExportSource{
		SelectionResolver: c.leadSelection(),
		leads:             c.repositories.lead,
		contacts:          c.repositories.lead,
		definitions:       c.repositories.customField,
		names:             c.actorNames().Names,
	})
}

var _ leadaction_usecase.Audiences = (*adsuc.AudienceUseCase)(nil)
