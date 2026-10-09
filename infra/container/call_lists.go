package container

import (
	"context"
	"log"

	calllisthttp "vozko/delivery/http/calllist"
	"vozko/domain/lead"
	calllist_repository "vozko/infra/repositories/calllist"
	lead_repository "vozko/infra/repositories/lead"
	calllist_usecase "vozko/usecases/calls/calllist"
)

type callListBundle struct {
	service *calllist_usecase.Service
}

type callListLeads struct {
	records   lead.Store
	directory *lead_repository.NumberDirectory
}

func (l callListLeads) Load(ctx context.Context, workspaceID, id string) (*lead.Lead, error) {
	return l.records.Load(ctx, workspaceID, id)
}

func (l callListLeads) LoadManyForDial(ctx context.Context, workspaceID string, ids []string) ([]*lead.Lead, error) {
	return l.directory.LoadManyForDial(ctx, workspaceID, ids)
}

func (l callListLeads) FindIdentities(ctx context.Context, workspaceID string, numbers []string) ([]*lead.Lead, error) {
	return l.directory.FindIdentities(ctx, workspaceID, numbers)
}

func (c *Container) callLists() *calllist_usecase.Service {
	if c.callListBundle != nil {
		return c.callListBundle.service
	}
	if c.services.conversationAuthImpl == nil {
		log.Fatalf("[call-lists] call lists need the workspace permission checker and the conversation access rule")
	}
	if c.repositories.lead == nil || c.repositories.callCDR == nil || c.repositories.workspaceConfig == nil {
		log.Fatalf("[call-lists] call lists need the lead, call record and workspace config repositories")
	}
	service, err := calllist_usecase.NewService(calllist_usecase.Deps{
		Store:        calllist_repository.NewStore(c.db),
		Leads:        callListLeads{records: c.repositories.lead, directory: lead_repository.NewNumberDirectory(c.db)},
		Snapshots:    c.repositories.lead,
		Calls:        c.repositories.callCDR,
		Outcomes:     outcomeCaptureReader{configs: c.repositories.workspaceConfig},
		Interactions: calllist_repository.NewInteractions(c.db),
		Access:       c.services.conversationAuthImpl,
		Lines:        c.leadDialTargets(),
		Permissions:  c.services.conversationAuthImpl,
	})
	if err != nil {
		log.Fatalf("[call-lists] %v", err)
	}
	c.callListBundle = &callListBundle{service: service}
	return service
}

func (c *Container) callListHandler() *calllisthttp.Handler {
	return calllisthttp.NewHandler(c.callLists())
}
