package container

import (
	"log"

	lead_domain "vozko/domain/lead"
	cmuc "vozko/usecases/carriermessaging"
	"vozko/usecases/carrierworkflow"
	workflow_usecase "vozko/usecases/workflow"
	"vozko/usecases/workflow/node_executors"
)

type workflowLeadTriggerBundle struct {
	Birthday  *workflow_usecase.BirthdaySweep
	LeadStart workflow_usecase.WebhookLeadStart
}

type emailLeads interface {
	FindByEmail(workspaceID, email string) (*lead_domain.Lead, error)
}

type webhookLeads struct {
	cmuc.LeadResolver
	emails emailLeads
}

func (l webhookLeads) FindByEmail(workspaceID, email string) (*lead_domain.Lead, error) {
	return l.emails.FindByEmail(workspaceID, email)
}

func (c *Container) buildWorkflowLeadTriggers(engine *workflow_usecase.RunEngine) workflowLeadTriggerBundle {
	if c.repositories.lead == nil || c.repositories.workspaceConfig == nil {
		log.Fatalf("[workflow] lead triggers need the leads and the workspace config")
	}
	shared := c.redisProvider.SharedState()
	starter, err := workflow_usecase.NewLeadRunStarter(c.repositories.workflowRun, workflow_usecase.NewEngineRunLauncher(engine, shared), shared)
	if err != nil {
		log.Fatalf("[workflow] lead run starter: %v", err)
	}
	workspaces, ok := c.repositories.workflow.(workflow_usecase.TriggerWorkspaces)
	if !ok {
		log.Fatalf("[workflow] the workflow repository cannot list the workspaces with an active trigger")
	}
	birthday, err := workflow_usecase.NewBirthdaySweep(workflow_usecase.BirthdaySweepDeps{
		Workspaces: workspaces,
		Workflows:  c.repositories.workflow,
		Leads:      c.repositories.lead,
		Timezones:  cmuc.NewWorkspaceTimezones(c.repositories.workspaceConfig),
		Starter:    starter,
	})
	if err != nil {
		log.Fatalf("[workflow] birthday sweep: %v", err)
	}
	return workflowLeadTriggerBundle{
		Birthday: birthday,
		LeadStart: workflow_usecase.WebhookLeadStart{
			Leads:   webhookLeads{LeadResolver: cmuc.NewLeadResolver(c.repositories.lead), emails: c.repositories.lead},
			Starter: starter,
		},
	}
}

func bindWorkflowCarrier(relay *node_executors.CarrierSenderRelay, carrier *carrierBundle) {
	if relay == nil || carrier == nil || carrier.Service == nil {
		log.Fatalf("[workflow] the RCS and SMS node needs the carrier service")
	}
	relay.Bind(carrierworkflow.NewSender(carrier.Service))
}
