package workflow

import (
	"encoding/json"
	"reflect"
	"strings"
	"testing"
)

func TestTheSpecListsTheEventsAWebhookTriggerAccepts(t *testing.T) {
	cases := []struct {
		name    string
		trigger Node
		want    []string
	}{
		{"a webhook filtering events lists them as production compares them", Node{ID: "t", Type: NodeTypeTriggerWebhook, Config: map[string]interface{}{WebhookEventsKey: []interface{}{" Pedido.Pago ", "carrinho"}}}, []string{"pedido.pago", "carrinho"}},
		{"a webhook without a filter accepts every event, so the list is empty", Node{ID: "t", Type: NodeTypeTriggerWebhook}, []string{}},
		{"an event filter the trigger cannot read promises nothing", Node{ID: "t", Type: NodeTypeTriggerWebhook, Config: map[string]interface{}{WebhookEventsKey: "pedido.pago"}}, []string{}},
		{"only webhooks receive events", Node{ID: "t", Type: NodeTypeTriggerFormSubmitted}, []string{}},
		{"a stage trigger has no events either", Node{ID: "t", Type: NodeTypeTriggerStageAdded}, []string{}},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			spec := onlySpec(t, triggerGraph(tc.trigger), TriggerInputSources{})
			if !reflect.DeepEqual(spec.AcceptedEvents, tc.want) {
				t.Fatalf("acceptedEvents = %#v, want %#v", spec.AcceptedEvents, tc.want)
			}
		})
	}
}

func TestTheAcceptedEventsAreAlwaysAJSONList(t *testing.T) {
	spec := onlySpec(t, triggerGraph(Node{ID: "t", Type: NodeTypeTriggerBirthday}), TriggerInputSources{})
	encoded, err := json.Marshal(spec)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(encoded), `"acceptedEvents":[]`) {
		t.Fatalf("spec = %s, want acceptedEvents as an empty list, never null", encoded)
	}
}
