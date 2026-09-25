package whatsapp_campaign_entry

import (
	"strings"

	"gorm.io/gorm"

	"vozko/domain/campaign"
	"vozko/domain/conversation"
	"vozko/domain/shared"
	wce "vozko/domain/whatsapp_campaign_entry"
)

const scopePlaceholder = "{scope}"

const campaignScopeSQL = `e.campaign_id = @campaign`

const workspaceScopeSQL = `e.campaign_id IN (
		SELECT c.id FROM whatsapp_campaigns c
		WHERE c.workspace_id = @workspace AND c.deleted_at IS NULL{campaignFilters}
	) AND e.created_at >= @scopeFrom AND e.created_at < @scopeTo`

const repliedSQL = `EXISTS (
		SELECT 1 FROM conversation_messages m
		WHERE m.entry_id = e.id AND m.entry_type = @entryType AND m.deleted_at IS NULL AND m.message_type IN @inbound
	)`

const stageCountsSQL = `COUNT(*) AS base,
	COUNT(*) FILTER (WHERE e.sent_at IS NOT NULL OR e.status IN @sent) AS sent,
	COUNT(*) FILTER (WHERE e.delivered_at IS NOT NULL OR e.status IN @delivered) AS delivered,
	COUNT(*) FILTER (WHERE e.read_at IS NOT NULL OR e.status IN @read) AS read,
	COUNT(*) FILTER (WHERE ` + repliedSQL + `) AS replied,
	COUNT(*) FILTER (WHERE e.status = @failed) AS failed`

const funnelSQL = `
SELECT ` + stageCountsSQL + `,
	COUNT(*) FILTER (WHERE e.status = @awaiting) AS awaiting_delivery,
	COUNT(*) FILTER (WHERE e.status = @pending) AS pending,
	COUNT(*) FILTER (WHERE e.status = @notEligible) AS not_eligible,
	MIN(e.sent_at) AS tracked_since
FROM whatsapp_campaign_entries e
WHERE ` + scopePlaceholder + ` AND e.deleted_at IS NULL`

const campaignsSQL = `
SELECT e.campaign_id, c.name AS campaign_name, ` + stageCountsSQL + `
FROM whatsapp_campaign_entries e
JOIN whatsapp_campaigns c ON c.id = e.campaign_id
WHERE ` + scopePlaceholder + ` AND e.deleted_at IS NULL
GROUP BY e.campaign_id, c.name
ORDER BY base DESC, c.name
LIMIT @limit`

const dailySQL = `
WITH scoped AS (
	SELECT e.id, e.sent_at, e.delivered_at, e.read_at
	FROM whatsapp_campaign_entries e
	WHERE ` + scopePlaceholder + ` AND e.deleted_at IS NULL
), replies AS (
	SELECT r.first_at FROM scoped s
	CROSS JOIN LATERAL (
		SELECT MIN(m.created_at) AS first_at FROM conversation_messages m
		WHERE m.entry_id = s.id AND m.entry_type = @entryType AND m.deleted_at IS NULL AND m.message_type IN @inbound
		  AND m.created_at < @to
	) r
), events AS (
	SELECT 'sent' AS kind, sent_at AS happened_at FROM scoped
	UNION ALL SELECT 'delivered', delivered_at FROM scoped
	UNION ALL SELECT 'read', read_at FROM scoped
	UNION ALL SELECT 'replied', first_at FROM replies
)
SELECT to_char(happened_at AT TIME ZONE @tz, 'YYYY-MM-DD') AS day,
	COUNT(*) FILTER (WHERE kind = 'sent') AS sent,
	COUNT(*) FILTER (WHERE kind = 'delivered') AS delivered,
	COUNT(*) FILTER (WHERE kind = 'read') AS read,
	COUNT(*) FILTER (WHERE kind = 'replied') AS replied
FROM events
WHERE happened_at >= @from AND happened_at < @to
GROUP BY 1
ORDER BY 1`

const failureReasonsSQL = `
SELECT e.error_code AS code, COUNT(*) AS count
FROM whatsapp_campaign_entries e
WHERE ` + scopePlaceholder + ` AND e.deleted_at IS NULL AND e.status = @failed
GROUP BY e.error_code
ORDER BY count DESC, code`

const tagsSQL = `
SELECT l.id AS label_id, l.name, l.color, COUNT(DISTINCT el.entry_id) AS count
FROM entry_labels el
JOIN labels l ON l.id = el.label_id::text AND l.deleted_at IS NULL
JOIN whatsapp_campaign_entries e ON e.id = el.entry_id AND e.deleted_at IS NULL
WHERE ` + scopePlaceholder + ` AND el.entry_type = @entryType AND el.deleted_at IS NULL
GROUP BY l.id, l.name, l.color
ORDER BY count DESC, l.name
LIMIT @limit`

type dispatchReportReader struct {
	db *gorm.DB
}

func NewDispatchReportReader(db *gorm.DB) wce.DispatchReportReader {
	return &dispatchReportReader{db: db}
}

func (r *dispatchReportReader) Funnel(scope wce.ReportScope) (wce.Funnel, error) {
	var out wce.Funnel
	sql, args := scoped(funnelSQL, scope, stageArgs(map[string]interface{}{
		"awaiting":    string(wce.SendStatusSent),
		"pending":     string(wce.SendStatusPending),
		"notEligible": string(wce.SendStatusNotEligiblePossibleSpam),
	}))
	err := r.db.Raw(sql, args).Scan(&out).Error
	return out, err
}

func (r *dispatchReportReader) Campaigns(scope wce.ReportScope, limit int) ([]wce.CampaignFunnel, error) {
	var out []wce.CampaignFunnel
	sql, args := scoped(campaignsSQL, scope, stageArgs(map[string]interface{}{"limit": limit}))
	err := r.db.Raw(sql, args).Scan(&out).Error
	return out, err
}

func (r *dispatchReportReader) Daily(scope wce.ReportScope, window wce.DayWindow) ([]wce.DayCount, error) {
	var out []wce.DayCount
	sql, args := scoped(dailySQL, scope, map[string]interface{}{
		"entryType": string(shared.EntryTypeWhatsApp),
		"inbound":   conversation.InboundMessageTypeStrings(),
		"tz":        window.Location.String(),
		"from":      window.From,
		"to":        window.To,
	})
	err := r.db.Raw(sql, args).Scan(&out).Error
	return out, err
}

func (r *dispatchReportReader) FailureReasons(scope wce.ReportScope) ([]wce.FailureReasonCount, error) {
	var out []wce.FailureReasonCount
	sql, args := scoped(failureReasonsSQL, scope, map[string]interface{}{
		"failed": string(wce.SendStatusFailed),
	})
	err := r.db.Raw(sql, args).Scan(&out).Error
	return out, err
}

func (r *dispatchReportReader) Tags(scope wce.ReportScope, limit int) ([]wce.TagCount, error) {
	var out []wce.TagCount
	sql, args := scoped(tagsSQL, scope, map[string]interface{}{
		"entryType": string(shared.EntryTypeWhatsApp),
		"limit":     limit,
	})
	err := r.db.Raw(sql, args).Scan(&out).Error
	return out, err
}

func stageArgs(extra map[string]interface{}) map[string]interface{} {
	args := map[string]interface{}{
		"sent":      provingStatuses(campaign.MilestoneSent),
		"delivered": provingStatuses(campaign.MilestoneDelivered),
		"read":      provingStatuses(campaign.MilestoneRead),
		"failed":    string(wce.SendStatusFailed),
		"entryType": string(shared.EntryTypeWhatsApp),
		"inbound":   conversation.InboundMessageTypeStrings(),
	}
	for key, value := range extra {
		args[key] = value
	}
	return args
}

func scoped(template string, scope wce.ReportScope, args map[string]interface{}) (string, map[string]interface{}) {
	if scope.IsCampaign() {
		args["campaign"] = scope.CampaignID
		return strings.Replace(template, scopePlaceholder, campaignScopeSQL, 1), args
	}

	var filters strings.Builder
	if scope.ExcludedType != "" {
		filters.WriteString(" AND COALESCE(c.type, '') <> @excludedType")
		args["excludedType"] = scope.ExcludedType
	}
	if len(scope.DepartmentIDs) > 0 {
		filters.WriteString(" AND c.department_id IN @departments")
		args["departments"] = scope.DepartmentIDs
	}
	args["workspace"] = scope.WorkspaceID
	args["scopeFrom"] = scope.Window.From
	args["scopeTo"] = scope.Window.To
	clause := strings.Replace(workspaceScopeSQL, "{campaignFilters}", filters.String(), 1)
	return strings.Replace(template, scopePlaceholder, clause, 1), args
}

func provingStatuses(m campaign.Milestone) []string {
	return wce.StatusStrings(wce.StatusesProving(m))
}
