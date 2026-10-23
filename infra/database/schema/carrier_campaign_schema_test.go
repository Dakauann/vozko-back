package schema

import (
	"slices"
	"sync"
	"testing"

	gormschema "gorm.io/gorm/schema"
)

func TestCarrierCampaignModelsDeclareTheirKeys(t *testing.T) {
	cases := []struct {
		model   any
		index   string
		unique  bool
		columns []string
		where   string
	}{
		{&CarrierCampaign{}, "idx_carrier_campaigns_workspace_created", false, []string{"workspace_id", "created_at"}, ""},
		{&CarrierCampaign{}, "idx_carrier_campaigns_scheduled", false, []string{"status", "scheduled_at"}, "scheduled_at IS NOT NULL"},
		{&CarrierCampaignRun{}, "ux_carrier_campaign_runs_occurrence", true, []string{"campaign_id", "occurrence"}, ""},
		{&CarrierCampaignRun{}, "idx_carrier_campaign_runs_status", false, []string{"status"}, ""},
		{&CarrierCampaignRecipient{}, "ux_carrier_campaign_recipients_lead", true, []string{"run_id", "lead_id"}, ""},
		{&CarrierCampaignRecipient{}, "idx_carrier_campaign_recipients_pending", false, []string{"run_id", "status", "id"}, ""},
		{&CarrierCampaignRecipient{}, "idx_carrier_campaign_recipients_campaign", false, []string{"campaign_id", "variant"}, ""},
		{&CarrierCampaignRecipient{}, "ux_carrier_campaign_recipients_message", true, []string{"message_id"}, "message_id IS NOT NULL"},
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

func TestCarrierCampaignModelsListsEveryTable(t *testing.T) {
	want := []string{"carrier_campaigns", "carrier_campaign_runs", "carrier_campaign_recipients"}
	var got []string
	for _, model := range CarrierCampaignModels() {
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
