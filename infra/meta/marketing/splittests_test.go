package marketing

import (
	"context"
	"net/http"
	"reflect"
	"testing"
	"time"

	"vozko/domain/advertising"
)

func splitTest(level advertising.TestLevel) advertising.SplitTest {
	return advertising.SplitTest{
		Name:        "Criativos",
		Description: "A contra B",
		Level:       level,
		Cells: []advertising.TestCell{
			{Name: "A", ObjectIDs: []string{"S1"}, Share: 50},
			{Name: "B", ObjectIDs: []string{"S2", "S3"}, Share: 50},
		},
		StartAt:    time.Date(2026, 10, 3, 0, 0, 0, 0, time.UTC),
		EndAt:      time.Date(2026, 10, 10, 0, 0, 0, 0, time.UTC),
		Confidence: 90,
	}
}

func TestCreateSplitTestOnTheBusiness(t *testing.T) {
	g, calls := gatewayWith(t, routes(t, map[string]string{
		"GET /v26.0/act_9":          `{"account_id":"9","currency":"BRL","account_status":1,"business":{"id":"B1","name":"Loja"}}`,
		"POST /v26.0/B1/ad_studies": `{"id":"ST1"}`,
	}))
	id, err := g.CreateSplitTest(context.Background(), "tok", "9", splitTest(advertising.TestAdSets))
	if err != nil || id != "ST1" {
		t.Fatalf("id %q err %v", id, err)
	}
	call := (*calls)[1]
	expectForm(t, call, map[string]string{
		"name": "Criativos", "description": "A contra B", "type": "SPLIT_TEST", "start_time": "1790985600", "end_time": "1791590400",
	}, "confidence_level")
	jsonEqual(t, "cells", call.form.Get("cells"), `[{"name":"A","treatment_percentage":50,"adsets":[{"id":"S1"}]},
		{"name":"B","treatment_percentage":50,"adsets":[{"id":"S2"},{"id":"S3"}]}]`)
}

func TestCreateCampaignSplitTestCells(t *testing.T) {
	g, calls := gatewayWith(t, routes(t, map[string]string{
		"GET /v26.0/act_9":          `{"account_id":"9","currency":"BRL","account_status":1,"business":{"id":"B1"}}`,
		"POST /v26.0/B1/ad_studies": `{"id":"ST1"}`,
	}))
	if _, err := g.CreateSplitTest(context.Background(), "tok", "9", splitTest(advertising.TestCampaigns)); err != nil {
		t.Fatal(err)
	}
	jsonEqual(t, "cells", (*calls)[1].form.Get("cells"), `[{"name":"A","treatment_percentage":50,"campaigns":[{"id":"S1"}]},
		{"name":"B","treatment_percentage":50,"campaigns":[{"id":"S2"},{"id":"S3"}]}]`)
}

func TestCreateSplitTestFailures(t *testing.T) {
	g, calls := gatewayWith(t, ok(`{}`))
	if _, err := g.CreateSplitTest(context.Background(), "tok", "9", splitTest("ad")); err == nil || len(*calls) != 0 {
		t.Fatalf("err %v calls %d", err, len(*calls))
	}
	g, calls = gatewayWith(t, ok(`{"account_id":"9","currency":"BRL","account_status":1}`))
	if _, err := g.CreateSplitTest(context.Background(), "tok", "9", splitTest(advertising.TestAdSets)); err == nil || len(*calls) != 1 {
		t.Fatalf("err %v calls %d", err, len(*calls))
	}
}

func TestListSplitTests(t *testing.T) {
	g, calls := gatewayWith(t, ok(`{"data":[
		{"id":"ST1","name":"Criativos","description":"A contra B","type":"SPLIT_TEST","start_time":"2026-10-03T00:00:00+0000","end_time":"2026-10-10T00:00:00+0000",
		 "cells":{"data":[{"name":"A","treatment_percentage":50,"adsets":{"data":[{"id":"S1"}]}},{"name":"B","treatment_percentage":50,"adsets":["S2","S3"]}]}},
		{"id":"ST2","name":"Lift","type":"LIFT","start_time":"2026-10-03T00:00:00+0000","end_time":"2026-10-10T00:00:00+0000"}]}`))
	tests, err := g.ListSplitTests(context.Background(), "tok", "9")
	if err != nil {
		t.Fatal(err)
	}
	call := (*calls)[0]
	if call.method != http.MethodGet || call.path != "/v26.0/act_9/ad_studies" || call.query.Get("fields") != "id,name,description,type,start_time,end_time,cells{name,treatment_percentage,adsets,campaigns}" {
		t.Fatalf("call = %+v", call)
	}
	want := splitTest(advertising.TestAdSets)
	want.MetaID, want.Confidence = "ST1", 0
	if len(tests) != 1 || !reflect.DeepEqual(tests[0], want) {
		t.Fatalf("tests = %+v", tests)
	}
}

func TestListSplitTestsRejectsUnreadableStudies(t *testing.T) {
	bodies := map[string]string{
		"mixed levels": `{"data":[{"id":"ST1","type":"SPLIT_TEST","start_time":"2026-10-03T00:00:00+0000","end_time":"2026-10-10T00:00:00+0000",
			"cells":[{"name":"A","adsets":["S1"]},{"name":"B","campaigns":["C1"]}]}]}`,
		"empty cell": `{"data":[{"id":"ST1","type":"SPLIT_TEST","start_time":"2026-10-03T00:00:00+0000","end_time":"2026-10-10T00:00:00+0000",
			"cells":[{"name":"A"}]}]}`,
		"no window": `{"data":[{"id":"ST1","type":"SPLIT_TEST"}]}`,
	}
	for name, body := range bodies {
		t.Run(name, func(t *testing.T) {
			g, _ := gatewayWith(t, ok(body))
			if _, err := g.ListSplitTests(context.Background(), "tok", "9"); err == nil {
				t.Fatal("expected an error")
			}
		})
	}
}
