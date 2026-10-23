package workflow

import (
	"errors"
	"testing"
)

func TestWebhookStartOf(t *testing.T) {
	cases := []struct {
		name   string
		config map[string]interface{}
		want   WebhookStart
		ok     bool
	}{
		{"absent keeps the conversation", map[string]interface{}{}, WebhookStartConversation, true},
		{"nil config", nil, WebhookStartConversation, true},
		{"blank keeps the conversation", map[string]interface{}{WebhookStartOnKey: " "}, WebhookStartConversation, true},
		{"conversation", map[string]interface{}{WebhookStartOnKey: "conversation"}, WebhookStartConversation, true},
		{"lead", map[string]interface{}{WebhookStartOnKey: "lead"}, WebhookStartLead, true},
		{"unknown", map[string]interface{}{WebhookStartOnKey: "contact"}, "", false},
		{"not text", map[string]interface{}{WebhookStartOnKey: float64(1)}, "", false},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got, ok := WebhookStartOf(tc.config)
			if got != tc.want || ok != tc.ok {
				t.Fatalf("WebhookStartOf = %q, %v, want %q, %v", got, ok, tc.want, tc.ok)
			}
		})
	}
}

func TestWebhookAcceptsEvent(t *testing.T) {
	cases := []struct {
		name   string
		config map[string]interface{}
		event  string
		ok     bool
	}{
		{"no filter accepts any call", map[string]interface{}{}, "", true},
		{"empty filter accepts any call", map[string]interface{}{WebhookEventsKey: []interface{}{}}, "anything", true},
		{"listed event", map[string]interface{}{WebhookEventsKey: []interface{}{"abandoned_cart", "pedido_pago"}}, "pedido_pago", true},
		{"case and spaces do not matter", map[string]interface{}{WebhookEventsKey: []interface{}{"Abandoned_Cart"}}, "  abandoned_cart ", true},
		{"other event", map[string]interface{}{WebhookEventsKey: []interface{}{"abandoned_cart"}}, "order_paid", false},
		{"missing event with a filter", map[string]interface{}{WebhookEventsKey: []interface{}{"abandoned_cart"}}, "", false},
		{"unreadable filter accepts nothing", map[string]interface{}{WebhookEventsKey: "abandoned_cart"}, "abandoned_cart", false},
		{"filter with a blank entry accepts nothing", map[string]interface{}{WebhookEventsKey: []interface{}{"abandoned_cart", " "}}, "abandoned_cart", false},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if got := WebhookAcceptsEvent(tc.config, tc.event); got != tc.ok {
				t.Fatalf("WebhookAcceptsEvent = %v, want %v", got, tc.ok)
			}
		})
	}
}

func TestWebhookOccurrence(t *testing.T) {
	cases := []struct {
		name                                     string
		event, externalID, idempotency, delivery string
		want                                     string
	}{
		{"event and external id", "Abandoned_Cart", " cart-9 ", "idem-1", "d-1", "abandoned_cart/cart-9"},
		{"only the idempotency key", "abandoned_cart", "", "idem-1", "d-1", "key/idem-1"},
		{"nothing but the delivery", "", "", "", "d-1", "delivery/d-1"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if got := WebhookOccurrence(tc.event, tc.externalID, tc.idempotency, tc.delivery); got != tc.want {
				t.Fatalf("WebhookOccurrence = %q, want %q", got, tc.want)
			}
		})
	}
}

func TestWebhookLeadEventStartsOnTheLead(t *testing.T) {
	subject := LeadSubject{WorkspaceID: "ws-1", LeadID: "lead-1", LeadName: "Ana"}
	event := WebhookLeadEvent(subject, "abandoned_cart/cart-9", map[string]interface{}{"webhook": map[string]interface{}{"method": "POST"}})
	if event.EntryID != "lead-1" || event.EntryType != EntryTypeLead || event.TriggerType != TriggerWebhook || !event.StartsOnLead() || !event.StartsRunPerEvent() {
		t.Fatalf("event = %+v", event)
	}
	if event.Data[DataKeyLeadID] != "lead-1" || event.Data[DataKeyLeadName] != "Ana" || event.Data["webhook"] == nil {
		t.Fatalf("data = %+v", event.Data)
	}
	if event.OccurrenceKey() != "webhook/abandoned_cart/cart-9" {
		t.Fatalf("key = %q", event.OccurrenceKey())
	}
	if (TriggerEvent{TriggerType: TriggerWebhook, EntryType: "whatsapp", Occurrence: "x"}).OccurrenceKey() != "" {
		t.Fatal("a webhook on a conversation keeps its existing dedup")
	}
}

func TestValidateWebhookTriggers(t *testing.T) {
	cases := []struct {
		name   string
		config map[string]interface{}
		field  string
	}{
		{"defaults", map[string]interface{}{}, ""},
		{"lead with events", map[string]interface{}{WebhookStartOnKey: "lead", WebhookEventsKey: []interface{}{"abandoned_cart"}}, ""},
		{"unknown start", map[string]interface{}{WebhookStartOnKey: "contact"}, WebhookStartOnKey},
		{"events not a list", map[string]interface{}{WebhookEventsKey: "abandoned_cart"}, WebhookEventsKey},
		{"blank event", map[string]interface{}{WebhookEventsKey: []interface{}{"ok", ""}}, WebhookEventsKey},
		{"event not text", map[string]interface{}{WebhookEventsKey: []interface{}{float64(3)}}, WebhookEventsKey},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			g := &Graph{Nodes: []Node{{ID: "t", Type: NodeTypeTriggerWebhook, Config: tc.config}, {ID: "e", Type: NodeTypeEnd}}, Edges: []Edge{{Source: "t", Target: "e"}}}
			err := ValidateWebhookTriggers(g)
			if tc.field == "" {
				if err != nil {
					t.Fatalf("err = %v", err)
				}
				return
			}
			if !errors.Is(err, ErrWebhookTriggerInvalid) || nodeIDFromErr(err) != "t" || fieldFromErr(err) != tc.field {
				t.Fatalf("err = %v", err)
			}
			report := LintGraph(g, WorkflowTypeMessages, BuiltinDefinitions(), nil, nil)
			if !hasIssue(report, LintInvalidWebhookTrigger, "t") || report.IsGreen() {
				t.Fatalf("issues = %+v", report.Issues)
			}
		})
	}
}

func TestLintWarnsAboutConversationNodesOnLeadRuns(t *testing.T) {
	catalog := append(BuiltinDefinitions(),
		NodeDefinition{Type: "action_send_text", Category: NodeCategoryMessaging, NeedsConversation: true, Scopes: []NodeScope{NodeScopeWhatsApp}},
		NodeDefinition{Type: "action_http_request", Category: NodeCategoryAction, Scopes: []NodeScope{NodeScopeShared}},
	)
	graphFor := func(trigger Node) *Graph {
		return &Graph{Nodes: []Node{
			trigger,
			{ID: "say", Type: "action_send_text", Config: map[string]interface{}{}},
			{ID: "call", Type: "action_http_request", Config: map[string]interface{}{}},
			{ID: "e", Type: NodeTypeEnd},
		}, Edges: []Edge{{Source: trigger.ID, Target: "say"}, {Source: "say", Target: "call"}, {Source: "call", Target: "e"}}}
	}
	for _, trigger := range []Node{
		{ID: "t", Type: NodeTypeTriggerWebhook, Config: map[string]interface{}{WebhookStartOnKey: "lead"}},
		{ID: "t", Type: NodeTypeTriggerBirthday, Config: map[string]interface{}{}},
	} {
		report := LintGraph(graphFor(trigger), WorkflowTypeMessages, catalog, nil, nil)
		warned := issuesOf(report, LintNodeNeedsConversation)
		if len(warned) != 1 || warned[0].NodeID != "say" || warned[0].Severity != SeverityAdvisory {
			t.Fatalf("%s: issues = %+v", trigger.Type, report.Issues)
		}
	}
	conversation := LintGraph(graphFor(Node{ID: "t", Type: NodeTypeTriggerWebhook, Config: map[string]interface{}{}}), WorkflowTypeMessages, catalog, nil, nil)
	if len(issuesOf(conversation, LintNodeNeedsConversation)) != 0 {
		t.Fatalf("a webhook on a conversation warns about nothing: %+v", conversation.Issues)
	}
}

func TestWebhookTriggerDefinitionOffersTheLeadStart(t *testing.T) {
	def, ok := NodeCatalogMap(BuiltinDefinitions())[NodeTypeTriggerWebhook]
	if !ok {
		t.Fatal("the webhook trigger needs a definition")
	}
	fields := map[string]ConfigField{}
	for _, f := range def.ConfigSchema {
		fields[f.Key] = f
	}
	start, events := fields[WebhookStartOnKey], fields[WebhookEventsKey]
	if start.Type != "select" || len(start.Options) != 2 || start.Options[0].Value != string(WebhookStartConversation) || start.Options[1].Value != string(WebhookStartLead) {
		t.Fatalf("start_on = %+v", start)
	}
	if events.Type != "tags" || events.Required {
		t.Fatalf("events = %+v", events)
	}
	if def.DefaultConfig[WebhookStartOnKey] != string(WebhookStartConversation) {
		t.Fatalf("defaults = %+v", def.DefaultConfig)
	}
}

func issuesOf(report LintReport, code LintIssueCode) []LintIssue {
	var out []LintIssue
	for _, issue := range report.Issues {
		if issue.Code == code {
			out = append(out, issue)
		}
	}
	return out
}
