package container

import (
	"log"

	"vozko/domain/campaign"
	uwc "vozko/domain/unofficial_whatsapp_campaign"
)

func unofficialSchedules(campaigns uwc.Repository) campaign.ScheduleClaimer {
	claimer, ok := campaigns.(campaign.ScheduleClaimer)
	if !ok {
		log.Fatalf("[unofficial-whatsapp-campaign] the campaign repository cannot claim scheduled starts")
	}
	return claimer
}
