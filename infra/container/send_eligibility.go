package container

import (
	"log"

	lead_repository "vozko/infra/repositories/lead"
	"vozko/usecases/campaignguard"
)

func (c *Container) campaignSpamPolicy() campaignguard.SpamPolicy {
	policy, err := campaignguard.NewConfigSpamPolicy(c.repositories.workspaceConfig)
	if err != nil {
		log.Fatalf("[campaigns] the spam protection policy cannot be built: %v", err)
	}
	return campaignguard.NewMemoSpamPolicy(policy, campaignguard.WorkspaceMemoTTL, nil)
}

func (c *Container) campaignSpamGuard() *campaignguard.SpamGuard {
	guard, err := campaignguard.NewSpamGuard(c.campaignSpamPolicy(), c.repositories.leadCampaignSend, c.redisProvider.SharedState(), nil)
	if err != nil {
		log.Fatalf("[campaigns] the send cooldown guard cannot be built: %v", err)
	}
	return guard
}

func (c *Container) campaignEligibility() *campaignguard.Eligibility {
	eligibility, err := campaignguard.NewEligibility(lead_repository.NewSendFacts(c.db), c.campaignSpamGuard())
	if err != nil {
		log.Fatalf("[campaigns] the send eligibility check cannot be built: %v", err)
	}
	return eligibility
}
