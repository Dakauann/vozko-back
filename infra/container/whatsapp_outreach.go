package container

import (
	"log"

	balance_domain "vozko/domain/balance"
	billing_domain "vozko/domain/billing"
	business_metrics_domain "vozko/domain/business_metrics"
	conversation_domain "vozko/domain/conversation"
	whatsapp_template "vozko/domain/whatsapp/template"
	wc_domain "vozko/domain/whatsapp_campaign"
	whatsapp_outreach_domain "vozko/domain/whatsapp_outreach"
	workspace_template_access_domain "vozko/domain/workspace_template_access"
	template_usecase "vozko/usecases/whatsapp/template"
	whatsapp_outreach_usecase "vozko/usecases/whatsapp_outreach"
)

// whatsAppOutreachUseCases is cold outbound's whole use-case surface.
//
// Returned as a value and spread into the container's useCases literal rather
// than assigned onto it field by field: initUseCases builds that struct in ONE
// literal near the end, so a helper writing `c.useCases.x = …` earlier would
// dereference a nil pointer.
type whatsAppOutreachUseCases struct {
	billedTemplateSend        whatsapp_template.BilledTemplateSendUseCase
	reconcileTemplateSends    whatsapp_template.ReconcileSendAttemptsUseCase
	startOfficialConversation whatsapp_outreach_domain.StartOfficialConversationUseCase
	quoteTemplateSend         whatsapp_outreach_domain.QuoteTemplateSendUseCase
}

// whatsAppOutreachDeps are the pieces built as LOCALS inside initUseCases —
// the reserver, the history manager, the metric publisher — which therefore
// cannot be reached through c.services and have to be handed over.
type whatsAppOutreachDeps struct {
	consume       balance_domain.ConsumeWhatsappTemplateUseCase
	inflight      balance_domain.InflightReserver
	history       conversation_domain.MessageHistoryManager
	recordMetric  business_metrics_domain.RecordMetricUseCase
	alerter       billing_domain.OpsAlerter
	ensureOrganic wc_domain.EnsureOrganicCoexistenceCampaignUseCase
	templateGrant workspace_template_access_domain.CheckAccessUseCase
}

// buildWhatsAppOutreach wires the one paid template sender and the dialog above it.
//
// Every constructor validates its own billing dependencies and returns an error,
// and every error here is FATAL. That is the point of the feature: the failure
// this design exists to prevent is a mis-wired container sending paid templates
// for free, and the only way to guarantee that never happens is for the process
// to refuse to start.
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
		RecordMetric:   d.recordMetric,
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

	start, err := whatsapp_outreach_usecase.NewStartConversationUseCase(whatsapp_outreach_usecase.Deps{
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
	})
	if err != nil {
		log.Fatalf("[container] whatsapp outreach: %v", err)
	}
	built.startOfficialConversation = start

	return built
}
