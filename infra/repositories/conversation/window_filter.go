package conversation_repository

import (
	"fmt"
	"strings"
	"time"

	"vozko/domain/channel"
	fbdomain "vozko/domain/facebook"
	igdomain "vozko/domain/instagram"
	lmw "vozko/domain/lead_message_window"
	mldomain "vozko/domain/mercadolivre"
	"vozko/domain/shared"
	tgdomain "vozko/domain/telegram"
	ttdomain "vozko/domain/tiktok"
	uw "vozko/domain/unofficial_whatsapp"
	"vozko/infra/database/sqlexpr"
)

func (q channelQuery) windowOpenOn(entryIDColumn string) string {
	return fmt.Sprintf(q.WindowOpenPredicate, entryIDColumn)
}

func (q channelQuery) groupOn(entryIDColumn string) string {
	if q.GroupPredicate == "" {
		return ""
	}
	return fmt.Sprintf(q.GroupPredicate, entryIDColumn)
}

func (q channelQuery) groupCondition(groups bool, entryIDColumn string) string {
	matched := "1 = 0"
	if q.GroupPredicate != "" {
		matched = q.groupOn(entryIDColumn)
	}
	if groups {
		return "(" + matched + ")"
	}
	return "NOT (" + matched + ")"
}

func windowFilterCondition(open bool, entryTypeColumn, entryIDColumn string, entryType shared.EntryType) string {
	return channelFilterCondition(open, channelQuery.windowOpenOn, entryTypeColumn, entryIDColumn, entryType)
}

func groupFilterCondition(groups bool, entryTypeColumn, entryIDColumn string, entryType shared.EntryType) string {
	return channelFilterCondition(groups, channelQuery.groupOn, entryTypeColumn, entryIDColumn, entryType)
}

func channelFilterCondition(include bool, predicate func(channelQuery, string) string, entryTypeColumn, entryIDColumn string, entryType shared.EntryType) string {
	parts := make([]string, 0, len(channelQueries))
	for _, q := range channelQueries {
		if entryType != "" && q.EntryType != entryType {
			continue
		}
		condition := predicate(q, entryIDColumn)
		if condition == "" {
			continue
		}
		parts = append(parts, "("+entryTypeColumn+" = "+sqlexpr.Literal(string(q.EntryType))+" AND "+condition+")")
	}
	matched := "1 = 0"
	if len(parts) > 0 {
		matched = strings.Join(parts, " OR ")
	}
	if include {
		return "(" + matched + ")"
	}
	return "NOT (" + matched + ")"
}

func csvColumn(column string) string {
	return "string_to_array(replace(COALESCE(" + column + ", ''), ' ', ''), ',')"
}

func csvHasAll(column string, values ...string) string {
	return csvColumn(column) + " @> ARRAY[" + sqlexpr.LiteralList(values) + "]::text[]"
}

func csvHasAny(column string, values ...string) string {
	return csvColumn(column) + " && ARRAY[" + sqlexpr.LiteralList(values) + "]::text[]"
}

func repliedWithin(column string, horizon time.Duration) string {
	return fmt.Sprintf("%s > NOW() - INTERVAL '%d seconds'", column, int64(horizon/time.Second))
}

func replyClock(column string, caps channel.Capabilities) string {
	horizon := caps.ReplyHorizon(true)
	if horizon <= 0 {
		return "TRUE"
	}
	return repliedWithin(column, horizon)
}

var officialWindowOpen = `EXISTS (SELECT 1 FROM whatsapp_campaign_entries wce_w
		JOIN whatsapp_campaigns wc_w ON wc_w.id = wce_w.campaign_id
		JOIN lead_message_windows lmw ON lmw.lead_id = wce_w.lead_id
		  AND (wc_w.business_phone_id IS NULL OR lmw.business_phone_id = wc_w.business_phone_id)
		WHERE wce_w.id = %[1]s AND wce_w.deleted_at IS NULL
		  AND ` + repliedWithin("lmw.last_message_at", lmw.MessageWindowDuration) + `)`

var instagramWindowOpen = `EXISTS (SELECT 1 FROM instagram_conversations igc_w
		JOIN instagram_accounts iga_w ON iga_w.id = igc_w.ig_account_id AND iga_w.deleted_at IS NULL
		WHERE igc_w.id = %[1]s AND igc_w.deleted_at IS NULL
		  AND iga_w.status = ` + sqlexpr.Literal(string(igdomain.StatusConnected)) + `
		  AND ` + csvHasAll("iga_w.granted_scopes", igdomain.ScopeManageMessages) + `
		  AND iga_w.access_token IS NOT NULL
		  AND ` + replyClock("igc_w.last_customer_message_at", igdomain.Descriptor().Capabilities) + `)`

func facebookWindowOpen() string {
	scopes, tasks, _ := fbdomain.CapabilityRequirements(fbdomain.CapMessaging)
	taskNames := make([]string, len(tasks))
	for i, task := range tasks {
		taskNames[i] = string(task)
	}
	return `EXISTS (SELECT 1 FROM facebook_conversations fbc_w
		JOIN facebook_pages fbp_w ON fbp_w.id = fbc_w.page_id AND fbp_w.deleted_at IS NULL
		WHERE fbc_w.id = %[1]s AND fbc_w.deleted_at IS NULL
		  AND fbp_w.status = ` + sqlexpr.Literal(string(fbdomain.StatusConnected)) + `
		  AND ` + csvHasAll("fbp_w.granted_scopes", scopes...) + `
		  AND ` + csvHasAny("fbp_w.tasks", taskNames...) + `
		  AND fbp_w.page_token IS NOT NULL
		  AND ` + replyClock("fbc_w.last_customer_message_at", fbdomain.Descriptor(false).Capabilities) + `)`
}

var telegramWindowOpen = `EXISTS (SELECT 1 FROM telegram_conversations tgc_w
		JOIN telegram_accounts tga_w ON tga_w.id = tgc_w.account_id AND tga_w.deleted_at IS NULL
		LEFT JOIN telegram_contacts tgct_w ON tgct_w.id = tgc_w.contact_id AND tgct_w.deleted_at IS NULL
		WHERE tgc_w.id = %[1]s AND tgc_w.deleted_at IS NULL
		  AND tga_w.bot_token IS NOT NULL
		  AND tga_w.status IN (` + sqlexpr.LiteralList([]string{string(tgdomain.StatusActive), string(tgdomain.StatusWebhookFailing)}) + `)
		  AND (
		    (tga_w.mode = ` + sqlexpr.Literal(string(tgdomain.ModeBusiness)) + ` AND tga_w.business_enabled
		      AND COALESCE((tga_w.business_rights ->> 'can_reply')::boolean, false)
		      AND ` + repliedWithin("tgc_w.last_customer_message_at", tgdomain.BusinessMessagingWindow) + `)
		    OR (tga_w.mode <> ` + sqlexpr.Literal(string(tgdomain.ModeBusiness)) + ` AND tgct_w.id IS NOT NULL AND NOT tgct_w.blocked)
		  ))`

var webchatWindowOpen = `EXISTS (SELECT 1 FROM webchat_conversations wcc_w
		JOIN webchat_widgets wcw_w ON wcw_w.id = wcc_w.widget_id AND wcw_w.deleted_at IS NULL
		JOIN webchat_visitors wcv_w ON wcv_w.id = wcc_w.visitor_id AND wcv_w.deleted_at IS NULL
		WHERE wcc_w.id = %[1]s AND wcc_w.deleted_at IS NULL
		  AND NOT wcv_w.blocked)`

var unofficialWindowOpen = `EXISTS (SELECT 1 FROM unofficial_whatsapp_conversations uwc_w
		JOIN unofficial_whatsapp_instances uwi_w ON uwi_w.id = uwc_w.instance_id AND uwi_w.deleted_at IS NULL
		JOIN unofficial_whatsapp_contacts uwct_w ON uwct_w.id = uwc_w.contact_id AND uwct_w.deleted_at IS NULL
		WHERE uwc_w.id = %[1]s AND uwc_w.deleted_at IS NULL
		  AND uwi_w.status = ` + sqlexpr.Literal(string(uw.StatusConnected)) + `
		  AND (uwi_w.restriction_until IS NULL OR uwi_w.restriction_until <= NOW())
		  AND uwi_w.restriction_can_send_new IS DISTINCT FROM false
		  AND NOT uwct_w.blocked)`

var unofficialGroup = `EXISTS (SELECT 1 FROM unofficial_whatsapp_conversations uwc_g
		WHERE uwc_g.id = %[1]s AND uwc_g.deleted_at IS NULL AND uwc_g.is_group)`

var mercadoLivreWindowOpen = `EXISTS (SELECT 1 FROM mercadolivre_conversations mlc_w
		JOIN mercadolivre_accounts mla_w ON mla_w.id = mlc_w.account_id
		  AND mla_w.status = ` + sqlexpr.Literal(string(mldomain.StatusConnected)) + ` AND mla_w.deleted_at IS NULL
		WHERE mlc_w.id = %[1]s AND mlc_w.deleted_at IS NULL
		  AND mla_w.can_send_messages AND mla_w.access_token IS NOT NULL
		  AND mlc_w.window_open
		  AND (mlc_w.outbound_budget = 0 OR mlc_w.outbound_since_inbound < mlc_w.outbound_budget))`

var tiktokWindowOpen = `EXISTS (SELECT 1 FROM tiktok_conversations ttc_w
		JOIN tiktok_accounts tta_w ON tta_w.id = ttc_w.account_id
		  AND tta_w.status = ` + sqlexpr.Literal(string(ttdomain.StatusConnected)) + ` AND tta_w.deleted_at IS NULL
		WHERE ttc_w.id = %[1]s AND ttc_w.deleted_at IS NULL
		  AND tta_w.can_send_messages AND tta_w.access_token IS NOT NULL
		  AND ttc_w.conversation_id <> ''
		  AND (ttc_w.budget_closed_at IS NULL OR ttc_w.last_customer_message_at > ttc_w.budget_closed_at)
		  AND ttc_w.outbound_since_inbound < ` + fmt.Sprint(ttdomain.OutboundBudget) + `
		  AND ` + replyClock("ttc_w.last_customer_message_at", ttdomain.ChannelCapabilities()) + `)`
