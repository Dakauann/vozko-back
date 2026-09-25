package whatsapp_campaign_test

import (
	"reflect"
	"testing"

	wc "vozko/domain/whatsapp_campaign"
)

func TestDefaultMappingDetectsCommonHeaders(t *testing.T) {
	got := wc.DefaultMapping([]string{"Nome", " Telefone ", "VAR2", "var1", "cidade"}, 2)
	want := wc.ColumnMapping{Number: " Telefone ", Name: "Nome", Variables: []string{"var1", "VAR2"}}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("mapping = %+v", got)
	}
}

func TestDefaultMappingLeavesUnknownVariablesBlank(t *testing.T) {
	got := wc.DefaultMapping([]string{"phone", "var1"}, 3)
	if !reflect.DeepEqual(got.Variables, []string{"var1", "", ""}) {
		t.Fatalf("variables = %q", got.Variables)
	}
}

func TestStartableNeedsPendingNumbers(t *testing.T) {
	cases := []struct {
		metrics *wc.CampaignMetrics
		want    error
	}{
		{nil, wc.ErrCampaignNoNumbers},
		{&wc.CampaignMetrics{}, wc.ErrCampaignNoNumbers},
		{&wc.CampaignMetrics{TotalNumbers: 2, Processed: 2}, wc.ErrCampaignAllProcessed},
		{&wc.CampaignMetrics{TotalNumbers: 2, Pending: 1, Processed: 1}, nil},
	}
	for _, c := range cases {
		if err := (&wc.Campaign{Metrics: c.metrics}).Startable(); err != c.want {
			t.Fatalf("%+v: %v", c.metrics, err)
		}
	}
}
