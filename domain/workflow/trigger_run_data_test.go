package workflow

import (
	"encoding/json"
	"testing"
	"time"
)

func encodedRunData(t *testing.T, data map[string]interface{}) string {
	t.Helper()
	b, err := json.Marshal(data)
	if err != nil {
		t.Fatal(err)
	}
	return string(b)
}

func TestTheRunDataBuildersKeepTheShapesProductionStartersWrote(t *testing.T) {
	cases := []struct {
		name string
		got  map[string]interface{}
		want map[string]interface{}
	}{
		{
			"a webhook with a JSON body keeps the parsed body",
			WebhookRunData("POST", map[string]interface{}{"k": "v"}, []byte(`{"k":"v"}`)),
			map[string]interface{}{"webhook": map[string]interface{}{"method": "POST", "body": map[string]interface{}{"k": "v"}}},
		},
		{
			"a webhook without a JSON body keeps the raw text",
			WebhookRunData("POST", nil, []byte("plain")),
			map[string]interface{}{"webhook": map[string]interface{}{"method": "POST", "body": "plain"}},
		},
		{
			"a form answer is keyed form.<field> with the form and response ids",
			FormSubmittedRunData("f1", " r1 ", map[string]interface{}{"nome": "Ana", "curso": "ADS"}),
			map[string]interface{}{"form_id": "f1", "form_response_id": "r1", "form.nome": "Ana", "form.curso": "ADS"},
		},
		{
			"a stage trigger carries the stage id the evaluator matches",
			StageAddedRunData("s1"),
			map[string]interface{}{"stage_id": "s1"},
		},
		{
			"a birthday carries the local day",
			BirthdayRunData(time.Date(2026, 10, 22, 9, 0, 0, 0, time.UTC)),
			map[string]interface{}{"birthday": "2026-10-22"},
		},
		{
			"a lead subject adds the lead id and name",
			LeadSubject{LeadID: "l1", LeadName: "Ana"}.RunData(map[string]interface{}{"birthday": "2026-10-22"}),
			map[string]interface{}{"birthday": "2026-10-22", "lead_id": "l1", "lead_name": "Ana"},
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if got, want := encodedRunData(t, tc.got), encodedRunData(t, tc.want); got != want {
				t.Fatalf("got %s\nwant %s", got, want)
			}
		})
	}
}

func TestAFormSubmissionStampsTheIdsOnACopy(t *testing.T) {
	answers := map[string]interface{}{"form.nome": "Ana", "form_id": "stale"}
	data := FormSubmissionData(answers, "f1", " r1 ")
	if data["form_id"] != "f1" || data["form_response_id"] != "r1" || data["form.nome"] != "Ana" {
		t.Fatalf("data = %v", data)
	}
	if answers["form_id"] != "stale" {
		t.Fatal("the submitted answers must not be changed in place")
	}
}

func TestTheBirthdayEventIsBuiltFromTheSharedBuilders(t *testing.T) {
	day := time.Date(2026, 10, 22, 0, 0, 0, 0, time.UTC)
	event := BirthdayEvent(LeadSubject{WorkspaceID: "ws1", LeadID: "l1", LeadName: "Ana"}, day)
	want := LeadSubject{LeadID: "l1", LeadName: "Ana"}.RunData(BirthdayRunData(day))
	if encodedRunData(t, event.Data) != encodedRunData(t, want) || event.EntryID != "l1" || event.EntryType != EntryTypeLead {
		t.Fatalf("event = %+v", event)
	}
}
