package container

import (
	"log"

	"vozko/domain/callsession"
	lead_repository "vozko/infra/repositories/lead"
	lead_usecase "vozko/usecases/lead"
)

func (c *Container) leadDialTargets() *lead_usecase.DialTargets {
	if c.sipTrunks == nil || c.sipTrunks.Planner == nil || c.sipTrunks.DialLines == nil {
		log.Fatalf("[calls] lead dial targets need the SIP call planner and its lines")
	}
	if c.services.conversationAuthImpl == nil {
		log.Fatalf("[calls] lead dial targets need the workspace permission checker")
	}
	targets, err := lead_usecase.NewDialTargets(lead_usecase.DialTargetDeps{
		Leads:       lead_repository.NewNumberDirectory(c.db),
		Permissions: c.services.conversationAuthImpl,
		Planner:     c.sipTrunks.Planner,
		Lines:       c.sipTrunks.DialLines,
	})
	if err != nil {
		log.Fatalf("[calls] %v", err)
	}
	return targets
}

func (c *Container) callListItems() callsession.CallListItems {
	return c.callLists()
}
