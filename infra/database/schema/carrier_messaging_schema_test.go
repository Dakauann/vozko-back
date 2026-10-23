package schema

import (
	"slices"
	"sync"
	"testing"

	gormschema "gorm.io/gorm/schema"
)

func parsedIndexes(t *testing.T, model any) map[string]*gormschema.Index {
	t.Helper()
	parsed, err := gormschema.Parse(model, &sync.Map{}, gormschema.NamingStrategy{})
	if err != nil {
		t.Fatalf("parse schema: %v", err)
	}
	out := map[string]*gormschema.Index{}
	for _, idx := range parsed.ParseIndexes() {
		out[idx.Name] = idx
	}
	return out
}

func indexColumns(idx *gormschema.Index) []string {
	columns := make([]string, len(idx.Fields))
	for i, f := range idx.Fields {
		columns[i] = f.DBName
	}
	return columns
}

func TestRCSModelsDeclareTheirKeys(t *testing.T) {
	cases := []struct {
		model   any
		index   string
		unique  bool
		columns []string
		where   string
	}{
		{&RCSSender{}, "idx_rcs_senders_provider_profile", false, []string{"provider", "provider_profile_id"}, ""},
		{&RCSSender{}, "idx_rcs_senders_workspace", false, []string{"workspace_id", "created_at"}, ""},
		{&CarrierContact{}, "ux_carrier_contacts_workspace_phone", true, []string{"workspace_id", "phone"}, "deleted_at IS NULL"},
		{&RCSConversation{}, "idx_rcs_conversations_thread", true, []string{"workspace_id", "sender_id", "contact_id"}, "deleted_at IS NULL"},
		{&RCSConversation{}, "idx_rcs_conversations_workspace_lastmsg", false, []string{"workspace_id", "last_message_at"}, ""},
		{&SMSConversation{}, "idx_sms_conversations_thread", true, []string{"workspace_id", "sender_id", "contact_id"}, "deleted_at IS NULL"},
		{&SMSConversation{}, "idx_sms_conversations_workspace_lastmsg", false, []string{"workspace_id", "last_message_at"}, ""},
		{&SMSRoute{}, "ux_sms_routes_dedicated_number", true, []string{"number"}, "kind = 'dedicated' AND deleted_at IS NULL"},
		{&SMSRoute{}, "idx_sms_routes_workspace", false, []string{"workspace_id", "created_at"}, ""},
		{&CarrierMessage{}, "ux_carrier_messages_reference", true, []string{"reference"}, ""},
		{&CarrierMessage{}, "idx_carrier_messages_cap", false, []string{"workspace_id", "phone", "created_at"}, ""},
		{&CarrierMessage{}, "idx_carrier_messages_overdue", false, []string{"status", "expires_at"}, ""},
		{&CarrierMessage{}, "idx_carrier_messages_run", false, []string{"run_kind", "run_id"}, ""},
		{&CarrierMessage{}, "idx_carrier_messages_workspace_run", false, []string{"workspace_id", "run_kind", "created_at"}, ""},
		{&CarrierMessage{}, "idx_carrier_messages_parent", false, []string{"parent_message_id"}, ""},
		{&CarrierMessage{}, "idx_carrier_messages_sms_phone", false, []string{"phone", "created_at"}, "step = 'sms'"},
		{&CarrierMessageEvent{}, "ux_carrier_message_events_dedup", true, []string{"dedup_key"}, ""},
		{&CarrierMessageEvent{}, "idx_carrier_message_events_phone_kind", false, []string{"workspace_id", "phone", "kind", "occurred_at"}, ""},
		{&CarrierSuppression{}, "ux_carrier_suppressions_workspace_phone", true, []string{"workspace_id", "phone"}, ""},
	}
	for _, tc := range cases {
		t.Run(tc.index, func(t *testing.T) {
			idx, ok := parsedIndexes(t, tc.model)[tc.index]
			if !ok {
				t.Fatalf("index %s is not declared", tc.index)
			}
			if (idx.Class == "UNIQUE") != tc.unique {
				t.Fatalf("index %s unique = %v, want %v", tc.index, idx.Class == "UNIQUE", tc.unique)
			}
			if got := indexColumns(idx); !slices.Equal(got, tc.columns) {
				t.Fatalf("index %s columns = %v, want %v", tc.index, got, tc.columns)
			}
			if idx.Where != tc.where {
				t.Fatalf("index %s where = %q, want %q", tc.index, idx.Where, tc.where)
			}
		})
	}
}

func TestRCSMessageMarketingIsAFlagThatDefaultsToFalse(t *testing.T) {
	parsed, err := gormschema.Parse(&CarrierMessage{}, &sync.Map{}, gormschema.NamingStrategy{})
	if err != nil {
		t.Fatal(err)
	}
	field := parsed.LookUpField("marketing")
	if field == nil || !field.NotNull || field.DefaultValue != "false" {
		t.Fatalf("marketing column = %+v", field)
	}
}

func TestRCSSettingsIsKeyedByWorkspace(t *testing.T) {
	parsed, err := gormschema.Parse(&CarrierSettings{}, &sync.Map{}, gormschema.NamingStrategy{})
	if err != nil {
		t.Fatal(err)
	}
	if len(parsed.PrimaryFields) != 1 || parsed.PrimaryFields[0].DBName != "workspace_id" {
		t.Fatalf("primary key = %v, want workspace_id", parsed.PrimaryFieldDBNames)
	}
}

func TestRCSModelsListsEveryTable(t *testing.T) {
	want := []string{"rcs_senders", "sms_routes", "carrier_contacts", "rcs_conversations", "sms_conversations", "carrier_messages", "carrier_message_events", "carrier_suppressions", "carrier_settings"}
	var got []string
	for _, model := range CarrierMessagingModels() {
		parsed, err := gormschema.Parse(model, &sync.Map{}, gormschema.NamingStrategy{})
		if err != nil {
			t.Fatal(err)
		}
		got = append(got, parsed.Table)
	}
	if !slices.Equal(got, want) {
		t.Fatalf("tables = %v, want %v", got, want)
	}
}

func TestCarrierMessageChannelColumns(t *testing.T) {
	parsed, err := gormschema.Parse(&CarrierMessage{}, &sync.Map{}, gormschema.NamingStrategy{})
	if err != nil {
		t.Fatal(err)
	}
	cases := []struct {
		column       string
		notNull      bool
		defaultValue string
	}{
		{"channel", true, ""},
		{"text", true, ""},
		{"segments", true, "0"},
		{"operator", false, ""},
	}
	for _, tc := range cases {
		t.Run(tc.column, func(t *testing.T) {
			field := parsed.LookUpField(tc.column)
			if field == nil {
				t.Fatalf("column %s is not declared", tc.column)
			}
			if field.NotNull != tc.notNull || (tc.defaultValue != "" && field.DefaultValue != tc.defaultValue) {
				t.Fatalf("column %s = not null %v default %q", tc.column, field.NotNull, field.DefaultValue)
			}
		})
	}
}

func TestSendersAndRoutesCarryTheInboxAutomationColumns(t *testing.T) {
	want := map[string]string{
		"agent_id":               "uuid",
		"workflow_id":            "uuid",
		"enable_agent_responses": "false",
		"enable_workflow":        "false",
		"enable_analysis":        "false",
		"enable_auto_staging":    "false",
		"enable_auto_memory":     "false",
		"automation_disclosure":  "",
	}
	for _, model := range []any{&RCSSender{}, &SMSRoute{}} {
		parsed, err := gormschema.Parse(model, &sync.Map{}, gormschema.NamingStrategy{})
		if err != nil {
			t.Fatal(err)
		}
		for column, detail := range want {
			field := parsed.LookUpField(column)
			if field == nil {
				t.Fatalf("%s has no %s column", parsed.Table, column)
			}
			switch detail {
			case "uuid":
				if field.NotNull {
					t.Fatalf("%s.%s must be nullable", parsed.Table, column)
				}
			default:
				if !field.NotNull || field.DefaultValue != detail && !(detail == "" && field.DefaultValue == "''") {
					t.Fatalf("%s.%s not null %v default %q", parsed.Table, column, field.NotNull, field.DefaultValue)
				}
			}
		}
	}
}

func TestSendersAndRoutesCarryTheInboxContainerColumns(t *testing.T) {
	for _, model := range []any{&RCSSender{}, &SMSRoute{}} {
		parsed, err := gormschema.Parse(model, &sync.Map{}, gormschema.NamingStrategy{})
		if err != nil {
			t.Fatal(err)
		}
		for _, column := range []string{"department_id", "pipeline_id"} {
			field := parsed.LookUpField(column)
			if field == nil || field.NotNull || field.DataType != "uuid" {
				t.Fatalf("%s.%s = %+v", parsed.Table, column, field)
			}
		}
	}
}
