package campaignstamp

import (
	"time"

	"gorm.io/gorm"

	"vozko/domain/campaign"
)

func column(m campaign.Milestone) string {
	return string(m) + "_at"
}

func Stamp(status campaign.SendStatus, at time.Time) map[string]interface{} {
	milestones := status.Milestones()
	out := make(map[string]interface{}, len(milestones))
	for _, m := range milestones {
		col := column(m)
		out[col] = gorm.Expr("COALESCE("+col+", ?)", at)
	}
	return out
}

func WithStamp(updates map[string]interface{}, status campaign.SendStatus, at time.Time) map[string]interface{} {
	for col, value := range Stamp(status, at) {
		updates[col] = value
	}
	return updates
}

func Clear() map[string]interface{} {
	out := make(map[string]interface{}, len(campaign.AllMilestones()))
	for _, m := range campaign.AllMilestones() {
		out[column(m)] = nil
	}
	return out
}
