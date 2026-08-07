package schema

import (
	"sync"
	"testing"

	gormschema "gorm.io/gorm/schema"
)

// This channel's repositories and its partial indexes address columns by NAME,
// in raw SQL and in Updates maps. GORM derives those names from the Go field
// names, and its naming strategy is not the obvious lowercase-with-underscores:
// it first runs a common-initialism replacer.
//
// `JID` matches that replacer's `ID` entry and becomes `JId`, which snake-cases
// to `j_id` rather than `jid`. The failure surfaces at migration time as
// "column does not exist", and it surfaces only for whichever column happens to
// have an index — every OTHER mismatch would have run fine and then silently
// written to a column nothing reads.
//
// So the column names are pinned here rather than trusted.

func columnNames(t *testing.T, model any) map[string]string {
	t.Helper()
	parsed, err := gormschema.Parse(model, &sync.Map{}, gormschema.NamingStrategy{})
	if err != nil {
		t.Fatalf("parse schema: %v", err)
	}
	out := make(map[string]string, len(parsed.Fields))
	for _, field := range parsed.Fields {
		out[field.Name] = field.DBName
	}
	return out
}

func assertColumns(t *testing.T, model any, want map[string]string) {
	t.Helper()
	got := columnNames(t, model)
	for field, column := range want {
		actual, ok := got[field]
		if !ok {
			t.Errorf("field %s is not mapped to any column", field)
			continue
		}
		if actual != column {
			t.Errorf("field %s maps to column %q, but the SQL in this channel says %q",
				field, actual, column)
		}
	}
}

// The two that actually broke, plus every neighbour whose name could plausibly
// be mangled the same way.
func TestUnofficialWhatsAppInstanceColumnNames(t *testing.T) {
	assertColumns(t, &UnofficialWhatsAppInstance{}, map[string]string{
		// The bug: without an explicit column tag these become j_id and l_id.
		"JID": "jid",
		"LID": "lid",

		// Neighbours with initialisms or acronyms, verified rather than assumed.
		"ProviderInstanceID": "provider_instance_id",
		"DeliveryTokenHash":  "delivery_token_hash",
		"ProfilePicURL":      "profile_pic_url",
		"IsBusinessAcct":     "is_business_acct",
		"SendDelayMinMS":     "send_delay_min_ms",
		"SendDelayMaxMS":     "send_delay_max_ms",

		// Restriction columns, all written by a narrow Updates map.
		"RestrictionCanSendNew": "restriction_can_send_new",
		"RestrictionUntil":      "restriction_until",
		"RestrictionCheckedAt":  "restriction_checked_at",

		"LastPolledAt":         "last_polled_at",
		"LastDisconnectReason": "last_disconnect_reason",
	})
}

func TestUnofficialWhatsAppContactColumnNames(t *testing.T) {
	assertColumns(t, &UnofficialWhatsAppContact{}, map[string]string{
		"JID":              "jid",
		"LID":              "lid",
		"PhoneNumber":      "phone_number",
		"PictureURL":       "picture_url",
		"IsBusiness":       "is_business",
		"ProfileFetchedAt": "profile_fetched_at",
		"LeadID":           "lead_id",
		"InstanceID":       "instance_id",
	})
}

func TestUnofficialWhatsAppConversationColumnNames(t *testing.T) {
	assertColumns(t, &UnofficialWhatsAppConversation{}, map[string]string{
		"ChatID":                "chat_id",
		"IsGroup":               "is_group",
		"ConversationStatus":    "conversation_status",
		"AutomationEnabled":     "automation_enabled",
		"LastMessageAt":         "last_message_at",
		"LastCustomerMessageAt": "last_customer_message_at",
		"LastAgentMessageAt":    "last_agent_message_at",
	})
}

func TestUnofficialWhatsAppServerColumnNames(t *testing.T) {
	assertColumns(t, &UnofficialWhatsAppServer{}, map[string]string{
		"BaseURL":       "base_url",
		"AdminToken":    "admin_token",
		"InUse":         "in_use",
		"LastHealthyAt": "last_healthy_at",
		"WorkspaceID":   "workspace_id",
	})
}

// The table names are referenced verbatim in entry_sources, channel_queries,
// channel_sources, the ownership registry, the stage subquery and every partial
// index. A rename here silently removes the channel from the inbox and the
// board rather than failing loudly.
func TestUnofficialWhatsAppTableNames(t *testing.T) {
	cases := map[string]string{
		UnofficialWhatsAppServer{}.TableName():       "unofficial_whatsapp_servers",
		UnofficialWhatsAppInstance{}.TableName():     "unofficial_whatsapp_instances",
		UnofficialWhatsAppContact{}.TableName():      "unofficial_whatsapp_contacts",
		UnofficialWhatsAppConversation{}.TableName(): "unofficial_whatsapp_conversations",
	}
	for got, want := range cases {
		if got != want {
			t.Errorf("table name = %q, want %q", got, want)
		}
	}
}
