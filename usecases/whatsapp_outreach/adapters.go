package whatsapp_outreach

import (
	workspace_config "vozko/domain/workspace_config"
	"vozko/usecases/campaignguard"
)

func NewConfigSpamPolicy(configs workspace_config.Repository) (SpamPolicyReader, error) {
	return campaignguard.NewConfigSpamPolicy(configs)
}
