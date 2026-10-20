package whatsappform

import (
	"errors"
	"testing"
)

const flowsWebhook = `{
	"object": "whatsapp_business_account",
	"entry": [{"id": "waba-1", "time": 1760875200, "changes": [
		{"field": "flows", "value": {"event": "FLOW_STATUS_CHANGE", "message": "Flow Name changed status from PUBLISHED to THROTTLED", "flow_id": "flow-9", "old_status": "PUBLISHED", "new_status": "THROTTLED"}},
		{"field": "flows", "value": {"event": "ENDPOINT_ERROR_RATE", "message": "high", "flow_id": "flow-9", "error_rate": 0.12, "threshold": 0.1, "alert_state": "ACTIVATED"}},
		{"field": "messages", "value": {}}
	]}]
}`

func TestParseFlowWebhook(t *testing.T) {
	events, err := ParseFlowWebhook([]byte(flowsWebhook))
	if err != nil {
		t.Fatal(err)
	}
	if len(events) != 2 {
		t.Fatalf("events = %+v", events)
	}
	status := events[0]
	if status.MetaWABAID != "waba-1" || status.MetaFlowID != "flow-9" || status.Event != FlowStatusChange || status.NewStatus != "THROTTLED" || status.OldStatus != "PUBLISHED" {
		t.Fatalf("status event = %+v", status)
	}
	alert := events[1]
	if alert.Event != FlowEndpointErrorRate || !alert.Activated() || alert.Message != "high" {
		t.Fatalf("alert = %+v", alert)
	}
	if _, err := ParseFlowWebhook([]byte(`nope`)); !errors.Is(err, ErrFlowEventInvalid) {
		t.Fatalf("err = %v", err)
	}
}

func TestFlowEventsKnowWhatTheyMean(t *testing.T) {
	if (FlowEvent{Event: FlowStatusChange}).IsAlert() || !(FlowEvent{Event: FlowClientErrorRate}).IsAlert() {
		t.Fatal("status changes are not alerts; error rates are")
	}
	if (FlowEvent{Event: FlowEndpointLatency, AlertState: "DEACTIVATED"}).Activated() {
		t.Fatal("a deactivated alert is not active")
	}
	if (FlowEvent{Event: "SOMETHING_NEW"}).IsAlert() {
		t.Fatal("an unknown event is never treated as an alert")
	}
}
