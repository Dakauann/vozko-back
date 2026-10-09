package container

import (
	"log"

	leadhttp "vozko/delivery/http/lead"
	wsdelivery "vozko/delivery/ws"
	lead_domain "vozko/domain/lead"
	actor_repository "vozko/infra/repositories/actor"
	lead_repository "vozko/infra/repositories/lead"
	lead_memory_repository "vozko/infra/repositories/lead_memory"
	opportunity_repository "vozko/infra/repositories/opportunity"
	lead_usecase "vozko/usecases/lead"
)

func (c *Container) leadChangeNotifier() lead_domain.ChangeNotifier {
	if c.services.conversationHub == nil {
		log.Fatalf("[leads] the conversation hub must be wired before lead changes can be announced")
	}
	return c.leadNotifier()
}

func (c *Container) leadNotifier() *wsdelivery.LeadChangeNotifier {
	if c.services.conversationHub == nil {
		log.Fatalf("[leads] the conversation hub must be wired before lead changes can be announced")
	}
	return wsdelivery.NewLeadChangeNotifier(c.services.conversationHub, c.repositories.lead)
}

func (c *Container) leadIncomingMerge() *lead_usecase.IncomingMerge {
	merge, err := lead_usecase.NewIncomingMerge(c.repositories.lead, c.leadChangeNotifier())
	if err != nil {
		log.Fatalf("[leads] %v", err)
	}
	return merge
}

func (c *Container) leadCommandDeps() lead_usecase.CommandDeps {
	return lead_usecase.CommandDeps{
		Store:       c.repositories.lead,
		Permissions: c.services.conversationAuthImpl,
		Owners:      actor_repository.NewOwnerDirectory(c.db),
		Visibility:  c.useCases.memberVisibility,
		Notifier:    c.leadChangeNotifier(),
		Phones:      c.repositories.businessPhone,
		PhoneGrants: c.repositories.workspacePhoneAccess,
		Meta:        c.services.businessPhoneMetaAPI,
		Entries:     c.repositories.lead,
		Relations:   c.repositories.lead,
		Duplicates:  c.repositories.lead,
		Definitions: c.repositories.customField,
		Anonymizer:  c.repositories.lead,
	}
}

func (c *Container) leadHandler() *leadhttp.LeadHandler {
	if c.services.conversationAuthImpl == nil || c.useCases.memberVisibility == nil || c.useCases.personDeals == nil {
		log.Fatalf("[leads] the authorizer, member visibility and the deal scope must be wired before the lead routes")
	}
	commands, err := lead_usecase.NewCommands(c.leadCommandDeps())
	if err != nil {
		log.Fatalf("[leads] %v", err)
	}
	history, err := lead_usecase.NewHistory(lead_usecase.HistoryDeps{
		Leads:         c.repositories.lead,
		Entries:       c.repositories.wcEntry,
		Messages:      c.repositories.conversation,
		Analyses:      c.repositories.conversationAnalyses,
		Access:        c.services.conversationAuthImpl,
		Windows:       c.repositories.leadMessageWindow,
		CampaignNames: c.repositories.lead,
		Permissions:   c.services.conversationAuthImpl,
		Definitions:   c.repositories.customField,
		Relatives:     c.repositories.lead,
		EntryLeads:    c.repositories.lead,
		Owners:        c.actorNames(),
	})
	if err != nil {
		log.Fatalf("[leads] %v", err)
	}
	numbers := lead_repository.NewNumberDirectory(c.db)
	summaries, err := lead_usecase.NewDetailSummaries(lead_usecase.SummaryDeps{
		Permissions: c.services.conversationAuthImpl,
		Leads:       numbers,
		DealScopes:  c.useCases.personDeals,
		Deals:       opportunity_repository.NewLeadDealCounter(c.db),
		Memories:    lead_memory_repository.NewLeadCounter(c.db),
		Holders:     numbers,
	})
	if err != nil {
		log.Fatalf("[leads] %v", err)
	}
	pages := c.leadPages()
	sections := c.leadSections()
	var imports leadhttp.Imports
	if service := c.leadImports(); service != nil {
		imports = service
	}
	return leadhttp.NewLeadHandler(leadhttp.HandlerDeps{
		Leads:     c.useCases.leadQueries,
		Repo:      c.repositories.lead,
		Commands:  commands,
		History:   history,
		Summaries: summaries,
		Pages:     pages,
		Sections:  sections,
		Imports:   imports,
		Timeline:  c.leadTimeline(),
		Actions:   c.leadActions(),
		Locations: c.leadLocations(),
		Sends:     c.leadSends(),
	})
}

func (c *Container) leadPages() *lead_usecase.Pages {
	if c.services.conversationAuthImpl == nil {
		log.Fatalf("[leads] the authorizer must be wired before the lead list")
	}
	pages, err := lead_usecase.NewPages(lead_usecase.PageDeps{
		Leads:       c.useCases.leadQueries,
		Contacts:    c.repositories.lead,
		Permissions: c.services.conversationAuthImpl,
		Definitions: c.repositories.customField,
		Names:       c.actorNames(),
		Zones:       c.attendanceScheduleResolver(),
		Areas:       c.leadAreas(),
	})
	if err != nil {
		log.Fatalf("[leads] %v", err)
	}
	return pages
}

func (c *Container) leadSections() *lead_usecase.Sections {
	if c.services.conversationAuthImpl == nil {
		log.Fatalf("[leads] the authorizer must be wired before the lead sections")
	}
	sections, err := lead_usecase.NewSections(lead_usecase.SectionDeps{
		Reader:      c.repositories.lead,
		Permissions: c.services.conversationAuthImpl,
		Definitions: c.repositories.customField,
		Names:       c.actorNames(),
		Zones:       c.attendanceScheduleResolver(),
		Areas:       c.leadAreas(),
		Caching: lead_usecase.SectionCaching{
			Memo:     c.analyticsMemo(),
			Gate:     c.sharedAnalyticsGate(),
			Versions: c.leadGenerations(),
			TTL:      lead_usecase.DefaultSectionTTL,
		},
	})
	if err != nil {
		log.Fatalf("[leads] %v", err)
	}
	return sections
}
