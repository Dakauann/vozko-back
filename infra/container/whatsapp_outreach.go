package container

import (
	"log"

	balance_domain "vozko/domain/balance"
	billing_domain "vozko/domain/billing"
	conversation_domain "vozko/domain/conversation"
	whatsapp_template "vozko/domain/whatsapp/template"
	wc_domain "vozko/domain/whatsapp_campaign"
	whatsapp_outreach_domain "vozko/domain/whatsapp_outreach"
	workspace_template_access_domain "vozko/domain/workspace_template_access"
	template_usecase "vozko/usecases/whatsapp/template"
	whatsapp_outreach_usecase "vozko/usecases/whatsapp_outreach"
)

type whatsAppOutreachUseCases struct {
	billedTemplateSend        whatsapp_template.BilledTemplateSendUseCase
	reconcileTemplateSends    whatsapp_template.ReconcileSendAttemptsUseCase
	startOfficialConversation whatsapp_outreach_domain.StartOfficialConversationUseCase
	conversationTemplate      whatsapp_outreach_domain.ConversationTemplateUseCase
	quoteTemplateSend         whatsapp_outreach_domain.QuoteTemplateSendUseCase
}

type whatsAppOutreachDeps struct {
	consume       balance_domain.ConsumeWhatsappTemplateUseCase
	inflight      balance_domain.InflightReserver
	history       conversation_domain.MessageHistoryManager
	alerter       billing_domain.OpsAlerter
	ensureOrganic wc_domain.EnsureOrganicCoexistenceCampaignUseCase
	templateGrant workspace_template_access_domain.CheckAccessUseCase
}

func (c *Container) buildWhatsAppOutreach(d whatsAppOutreachDeps) whatsAppOutreachUseCases {
	var built whatsAppOutreachUseCases

	sender, err := template_usecase.NewBilledTemplateSendUseCase(template_usecase.BilledTemplateSenderDeps{
		Templates:      c.repositories.whatsappTemplate,
		Attempts:       c.repositories.whatsappTemplateSend,
		ClientFactory:  c.services.whatsappClientFactory,
		Consume:        d.consume,
		Ledger:         c.repositories.balance,
		Inflight:       d.inflight,
		BalanceChecker: c.services.cachedBalanceChecker,
		Alerter:        d.alerter,
	})
	if err != nil {
		log.Fatalf("[container] whatsapp outreach: %v", err)
	}
	built.billedTemplateSend = sender

	built.reconcileTemplateSends = template_usecase.NewReconcileSendAttemptsUseCase(
		c.repositories.whatsappTemplateSend, d.consume, c.repositories.balance, d.alerter)

	built.quoteTemplateSend = whatsapp_outreach_usecase.NewQuoteUseCase(
		c.repositories.whatsappTemplate, d.consume, c.services.cachedBalanceChecker)

	deps := whatsapp_outreach_usecase.Deps{
		Phones:        c.repositories.businessPhone,
		PhoneGrants:   c.repositories.workspacePhoneAccess,
		Templates:     c.repositories.whatsappTemplate,
		TemplateGrant: d.templateGrant,
		Leads:         c.repositories.lead,
		Entries:       c.repositories.wcEntry,
		Campaigns:     c.repositories.wcCampaign,
		EnsureOrganic: d.ensureOrganic,
		Windows:       c.repositories.leadMessageWindow,
		CampaignSends: c.repositories.leadCampaignSend,
		SpamPolicy:    whatsapp_outreach_usecase.NewConfigSpamPolicy(c.repositories.workspaceConfig),
		History:       d.history,
		Sender:        sender,
		Limiter:       whatsapp_outreach_usecase.NewSharedStateLimiter(c.redisProvider.SharedState()),
	}

	if built.startOfficialConversation, err = whatsapp_outreach_usecase.NewStartConversationUseCase(deps); err != nil {
		log.Fatalf("[container] whatsapp outreach: %v", err)
	}
	if built.conversationTemplate, err = whatsapp_outreach_usecase.NewConversationTemplateUseCase(deps); err != nil {
		log.Fatalf("[container] whatsapp outreach: %v", err)
	}

	return built
}
