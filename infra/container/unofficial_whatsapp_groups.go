package container

import (
	uwhttp "vozko/delivery/http/unofficial_whatsapp"
	uwrepo "vozko/infra/repositories/unofficial_whatsapp"
	uwgrepo "vozko/infra/repositories/unofficial_whatsapp_group"
	uwuc "vozko/usecases/unofficial_whatsapp"
	uwguc "vozko/usecases/unofficial_whatsapp_group"
)

func (c *Container) initWorkspaceGroups(bundle *unofficialWhatsAppBundle, webhook *uwuc.HandleWebhookUseCase) {
	bundle.WorkspaceGroups = uwguc.New(uwguc.Deps{
		Groups:    uwgrepo.NewGroupRepository(c.db),
		Members:   uwgrepo.NewMemberRepository(c.db),
		Stats:     uwgrepo.NewStatsReader(c.db),
		Analyses:  uwgrepo.NewAnalysisRepository(c.db),
		Runs:      uwgrepo.NewRunRepository(c.db),
		Joins:     uwgrepo.NewJoinRepository(c.db),
		Messages:  uwgrepo.NewMessageReader(c.db),
		Campaigns: uwgrepo.NewCampaignReader(c.db),
		Instances: bundle.Instances,
		Servers:   bundle.Servers,
		Seats:     bundle.Groups,
		GroupAPI:  bundle.GroupAPI,
		SeatSync:  bundle.GroupUseCases,
		Leads:     uwrepo.NewLeadLinker(c.repositories.lead),
		Locations: c.attendanceScheduleResolver(),
		AI:        c.services.ai,
		Model:     c.cfg.OpenRouterDefaultModel,
		Balance:   c.services.cachedBalanceChecker,
		Owner:     c.replicaID,
	})
	webhook.SetGroupWatcher(bundle.WorkspaceGroups)
	bundle.WorkspaceGroupHandler = uwhttp.NewWorkspaceGroupHandler(bundle.WorkspaceGroups, c.services.conversationAuthImpl)
}

func unofficialWhatsAppWorkspaceGroupHandler(c *Container) *uwhttp.WorkspaceGroupHandler {
	if c.unofficialWhatsApp == nil || !c.unofficialWhatsApp.Enabled {
		return nil
	}
	return c.unofficialWhatsApp.WorkspaceGroupHandler
}
