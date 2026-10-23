package carriercampaign_repository

import (
	"gorm.io/gorm"

	"vozko/domain/campaign"
	"vozko/infra/repositories/campaignstamp"
)

var carrierTimeline = campaignstamp.TimelineRelation{
	From:  "carrier_campaign_recipients r JOIN carrier_messages e ON e.id = r.message_id",
	Scope: "r.campaign_id = ? AND e.parent_message_id IS NULL",
	Columns: map[campaign.Milestone]string{
		campaign.MilestoneSent:      "e.accepted_at",
		campaign.MilestoneDelivered: "e.delivered_at",
		campaign.MilestoneRead:      "e.read_at",
		campaign.MilestoneFailed:    "e.failed_at",
	},
}

func NewTimelineReader(db *gorm.DB) *campaignstamp.TimelineReader {
	return campaignstamp.NewRelationTimelineReader(db, carrierTimeline)
}
