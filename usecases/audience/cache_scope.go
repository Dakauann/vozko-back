package audience_usecase

const (
	classifyFeature      = "audience_classify"
	authorRoleFeature    = "audience_author_role"
	alertBriefingFeature = "audience_alert_briefing"
	replyDraftFeature    = "audience_reply_draft"
)

func workspaceScope(feature, workspaceID string) string {
	return feature + ":" + workspaceID
}
