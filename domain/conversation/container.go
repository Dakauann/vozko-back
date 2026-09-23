package conversation

import "strings"

func ContainerIDOf(entry InboxEntry) string {
	if campaign := strings.TrimSpace(entry.CampaignID); campaign != "" {
		return campaign
	}
	return strings.TrimSpace(entry.BusinessPhoneID)
}
