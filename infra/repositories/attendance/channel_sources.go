package attendance_repository

import (
	"strings"

	"vozko/domain/shared"
)

// The attendance overview reads conversations straight from each channel's own
// tables, because it needs facts the shared message table does not carry: the
// conversation status, the owning department, and when the conversation was
// created rather than when it was last messaged.
//
// That read was written for WhatsApp and only WhatsApp. Its channel-mix panel
// counted `entry_type = 'whatsapp'` and therefore always reported 100% WhatsApp,
// and its entry CTE had exactly one branch, so an Instagram or Telegram
// conversation was invisible in every operational metric on the page. An agent
// who spent a day on Instagram looked idle.
//
// A channel is declared ONCE below and both halves of the CTE are generated from
// it. These are trusted, code-authored constants; every VALUE is still bound.

// channelSource declares how one channel's conversations are read for the
// attendance overview.
type channelSource struct {
	EntryType shared.EntryType

	// EntryTable and ContainerTable carry their aliases, e.g.
	// "whatsapp_campaign_entries wce".
	EntryTable     string
	EntryAlias     string
	ContainerTable string
	ContainerAlias string
	// ContainerJoin is the ON condition tying the entry to its container.
	ContainerJoin string

	// StatusColumn is the conversation-status column. Empty means the channel has
	// no status, and every row buckets as pending.
	StatusColumn string
	// CloseSourceColumn records who closed the conversation. Empty projects ''.
	CloseSourceColumn string
	// DepartmentColumn scopes to a department, on the container.
	DepartmentColumn string
	// ContainerIDColumn is what a container filter ("this campaign", "this
	// account") compares against.
	ContainerIDColumn string
	// WorkspaceColumn is where the tenant lives. WhatsApp carries it on the
	// campaign; the newer channels carry it on the conversation row itself.
	WorkspaceColumn string
}

// channelSources is the registry. To add a channel: append its declaration.
var channelSources = []channelSource{
	{
		EntryType:      shared.EntryTypeWhatsApp,
		EntryTable:     "whatsapp_campaign_entries wce",
		EntryAlias:     "wce",
		ContainerTable: "whatsapp_campaigns wc",
		ContainerAlias: "wc",
		ContainerJoin:  "wc.id = wce.campaign_id",

		StatusColumn:      "wce.conversation_status",
		CloseSourceColumn: "wce.close_source",
		DepartmentColumn:  "wc.department_id",
		ContainerIDColumn: "wce.campaign_id",
		WorkspaceColumn:   "wc.workspace_id",
	},
	{
		// The Instagram account is the container: it carries the department, and
		// a "campaign" filter on this channel means an account filter.
		EntryType:      shared.EntryTypeInstagram,
		EntryTable:     "instagram_conversations igc",
		EntryAlias:     "igc",
		ContainerTable: "instagram_accounts iga",
		ContainerAlias: "iga",
		ContainerJoin:  "iga.id = igc.ig_account_id",

		StatusColumn:      "igc.conversation_status",
		CloseSourceColumn: "igc.close_source",
		DepartmentColumn:  "iga.department_id",
		ContainerIDColumn: "igc.ig_account_id",
		WorkspaceColumn:   "igc.workspace_id",
	},
	{
		EntryType:      shared.EntryTypeTelegram,
		EntryTable:     "telegram_conversations tgc",
		EntryAlias:     "tgc",
		ContainerTable: "telegram_accounts tga",
		ContainerAlias: "tga",
		ContainerJoin:  "tga.id = tgc.account_id",

		StatusColumn:      "tgc.conversation_status",
		CloseSourceColumn: "tgc.close_source",
		DepartmentColumn:  "tga.department_id",
		ContainerIDColumn: "tgc.account_id",
		WorkspaceColumn:   "tgc.workspace_id",
	},
	{
		// The instance is the container: it carries the department, and a
		// "campaign" filter on this channel means an instance filter.
		EntryType:      shared.EntryTypeUnofficialWhatsApp,
		EntryTable:     "unofficial_whatsapp_conversations uwc",
		EntryAlias:     "uwc",
		ContainerTable: "unofficial_whatsapp_instances uwi",
		ContainerAlias: "uwi",
		ContainerJoin:  "uwi.id = uwc.instance_id",

		StatusColumn:      "uwc.conversation_status",
		CloseSourceColumn: "uwc.close_source",
		DepartmentColumn:  "uwi.department_id",
		ContainerIDColumn: "uwc.instance_id",
		WorkspaceColumn:   "uwc.workspace_id",
	},
}

// selectedChannelSources returns the channels a filter reads.
//
// An empty filter means every channel, which is the whole point: the overview
// is a workspace-wide view, and silently limiting it to one channel is how the
// page came to lie.
func selectedChannelSources(channel string) []channelSource {
	channel = strings.TrimSpace(channel)
	if channel == "" {
		return channelSources
	}
	for _, src := range channelSources {
		if string(src.EntryType) == channel {
			return []channelSource{src}
		}
	}
	// A named channel with no source (voice) reads nothing here rather than
	// falling back to everything, which would silently answer a different
	// question than the one asked.
	return nil
}

// statusBucket renders the CASE that buckets a conversation status.
func (s channelSource) statusBucket() string {
	if s.StatusColumn == "" {
		return "'pending'::text"
	}
	return `CASE
					WHEN ` + s.StatusColumn + ` = 'finished' THEN 'finished'
					WHEN ` + s.StatusColumn + ` = 'ongoing' THEN 'ongoing'
					ELSE 'pending'
				END`
}

func (s channelSource) closeSource() string {
	if s.CloseSourceColumn == "" {
		return "''"
	}
	return "COALESCE(" + s.CloseSourceColumn + ", '')"
}

// projection is the column list both halves of the CTE emit. It must be
// identical across channels or the UNION ALL is rejected by Postgres and the
// whole overview breaks, including for WhatsApp-only tenants.
func (s channelSource) projection(isNewContact string) string {
	return s.EntryAlias + `.id AS entry_id, '` + string(s.EntryType) + `'::text AS entry_type,
				` + s.statusBucket() + ` AS status_bucket,
				` + isNewContact + ` AS is_new_contact,
				EXTRACT(HOUR FROM (` + s.EntryAlias + `.created_at))::int AS hour_bucket,
				COALESCE(` + s.DepartmentColumn + `::text, '') AS department_id,
				COALESCE(ia.assigned_user_id::text, '') AS assigned_user_id,
				` + s.EntryAlias + `.created_at,
				` + s.closeSource() + ` AS close_source`
}

// groupByColumns lists the non-aggregated columns of the activity half.
func (s channelSource) groupByColumns() string {
	cols := []string{
		s.EntryAlias + ".id",
		s.EntryAlias + ".created_at",
		s.DepartmentColumn,
		"ia.assigned_user_id",
	}
	if s.StatusColumn != "" {
		cols = append(cols, s.StatusColumn)
	}
	if s.CloseSourceColumn != "" {
		cols = append(cols, s.CloseSourceColumn)
	}
	return strings.Join(cols, ", ")
}
