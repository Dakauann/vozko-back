package container

import (
	"log"
	"time"

	buttonactionhttp "vozko/delivery/http/buttonaction"
	redisCache "vozko/infra/cache"
	buttonaction_repository "vozko/infra/repositories/buttonaction"
	calllist_repository "vozko/infra/repositories/calllist"
	buttonaction_usecase "vozko/usecases/buttonaction"
	wc_usecase "vozko/usecases/whatsapp_campaign"
)

const buttonActionComputeTimeout = 5 * time.Second

func (c *Container) buttonActions() *buttonaction_usecase.Service {
	if c.buttonActionService != nil {
		return c.buttonActionService
	}
	if c.useCases.workspaceTemplates == nil || c.useCases.wcCampaignAccess == nil || c.services.conversationAuthImpl == nil {
		log.Fatalf("[button-actions] button actions need the workspace templates, the campaign access rule and the permission checker")
	}
	shared := c.redisProvider.SharedState()
	service, err := buttonaction_usecase.NewService(buttonaction_usecase.Deps{
		Templates:   c.useCases.workspaceTemplates,
		Campaigns:   c.useCases.wcCampaignAccess,
		Settings:    buttonaction_repository.NewTemplateSettingsStore(c.db),
		Routes:      buttonaction_repository.NewCampaignRoutesStore(c.db),
		Ownership:   c.buttonActionOwnership(),
		Permissions: c.services.conversationAuthImpl,
		Memo:        redisCache.NewSharedStateMemo(shared, buttonActionComputeTimeout),
		Versions:    redisCache.NewSharedStateVersions(shared, "buttonactions"),
	})
	if err != nil {
		log.Fatalf("[button-actions] %v", err)
	}
	c.buttonActionService = service
	return service
}

func (c *Container) wireButtonActions() {
	service := c.buttonActions()
	gated, ok := c.useCases.createWCCampaign.(interface {
		SetExitRiskGate(wc_usecase.ExitRiskGate)
	})
	if !ok {
		log.Fatalf("[button-actions] the official campaign creation cannot take the exit risk gate")
	}
	gated.SetExitRiskGate(service)
	if c.handlers == nil || c.handlers.whatsappTemplate == nil {
		log.Fatalf("[button-actions] the template handler must exist before the exit guard is wired")
	}
	c.handlers.whatsappTemplate.SetExitGuardedCreator(service)
}

func (c *Container) buttonActionHandler() *buttonactionhttp.Handler {
	return buttonactionhttp.NewHandler(c.buttonActions())
}

func (c *Container) buttonActionOwnership() *buttonaction_usecase.WorkspaceOwnership {
	ownership, err := buttonaction_usecase.NewWorkspaceOwnership(buttonaction_usecase.OwnershipDeps{
		Workflows:   c.repositories.workflow,
		CallLists:   calllist_repository.NewStore(c.db),
		Fields:      c.repositories.customField,
		Pipelines:   c.repositories.pipeline,
		Stages:      c.repositories.stage,
		Departments: c.repositories.workspaceDepartment,
		Members:     c.repositories.workspace,
		Automations: c.automationDirectory(),
	})
	if err != nil {
		log.Fatalf("[button-actions] checking action parameters needs the workflow, field, pipeline, stage, department and member repositories: %v", err)
	}
	return ownership
}
