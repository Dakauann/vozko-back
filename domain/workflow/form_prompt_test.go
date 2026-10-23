package workflow

import "testing"

func TestTheFormNodeIsAMessagingActionThatAwaitsTheContactButNotAReply(t *testing.T) {
	n := NodeTypeActionSendForm
	if n != "action_send_form" || !n.Valid() || !n.IsMessaging() || !n.IsFormPrompt() {
		t.Fatalf("%q must be a valid messaging node that sends a form", n)
	}
	if n.ParksForReply() {
		t.Error("an ordinary reply must never advance a form node")
	}
	if !n.AwaitsContact() {
		t.Error("a form node parks until the contact acts")
	}
	if n.IsWait() || n.IsTrigger() {
		t.Error("a form node is neither a wait node nor a trigger")
	}
}

func TestAwaitsContactCoversEveryNodeThatParksForTheContact(t *testing.T) {
	cases := []struct {
		node NodeType
		want bool
	}{
		{NodeTypeActionSendForm, true},
		{NodeTypeActionSendInteractive, true},
		{NodeTypeWaitForReply, true},
		{NodeTypeWaitDuration, true},
		{NodeTypeActionSendText, false},
		{NodeTypeTriggerFormSubmitted, false},
	}
	for _, tc := range cases {
		if got := tc.node.AwaitsContact(); got != tc.want {
			t.Errorf("%s.AwaitsContact() = %v, want %v", tc.node, got, tc.want)
		}
	}
}

func TestTheFormSubmittedTriggerIsAMessagesTrigger(t *testing.T) {
	if TriggerFormSubmitted != "trigger_form_submitted" || !TriggerFormSubmitted.Valid() {
		t.Fatalf("%q must be a valid trigger", TriggerFormSubmitted)
	}
	if TriggerFormSubmitted.WorkflowType() != WorkflowTypeMessages {
		t.Error("a form answer starts a messages workflow")
	}
	if NodeType(TriggerFormSubmitted) != NodeTypeTriggerFormSubmitted || !NodeTypeTriggerFormSubmitted.IsTrigger() || !NodeTypeTriggerFormSubmitted.Valid() {
		t.Error("the trigger node mirrors the trigger type")
	}
	if NodeTypeTriggerFormSubmitted.Category() != NodeCategoryTrigger {
		t.Error("the trigger node sits with the triggers")
	}
}

func TestOnlyTheFormTriggerStartsARunPerEvent(t *testing.T) {
	for _, trig := range []TriggerType{TriggerFirstMessage, TriggerMessageReceived, TriggerStageAdded, TriggerWebhook, TriggerCallReceived} {
		if (TriggerEvent{TriggerType: trig, EntryType: "whatsapp"}).StartsRunPerEvent() {
			t.Errorf("%s keeps one run per entry", trig)
		}
		if key := (TriggerEvent{TriggerType: trig, Data: map[string]interface{}{DataKeyFormResponseID: "resp-1"}}).OccurrenceKey(); key != "" {
			t.Errorf("%s has occurrence key %q", trig, key)
		}
	}
	if !(TriggerEvent{TriggerType: TriggerFormSubmitted, EntryType: "whatsapp"}).StartsRunPerEvent() {
		t.Error("each form response starts its own run")
	}
	cases := []struct {
		data map[string]interface{}
		want string
	}{
		{map[string]interface{}{DataKeyFormResponseID: " resp-1 "}, "resp-1"},
		{map[string]interface{}{}, ""},
		{nil, ""},
	}
	for _, tc := range cases {
		if got := (TriggerEvent{TriggerType: TriggerFormSubmitted, Data: tc.data}).OccurrenceKey(); got != tc.want {
			t.Errorf("OccurrenceKey(%v) = %q, want %q", tc.data, got, tc.want)
		}
	}
}

func TestFormTriggerAcceptsTheFormsItNames(t *testing.T) {
	cases := []struct {
		name   string
		config map[string]interface{}
		formID string
		want   bool
	}{
		{"no config means any form", nil, "f1", true},
		{"an empty list means any form", map[string]interface{}{FormTriggerFormIDsKey: []interface{}{}}, "f1", true},
		{"a listed form", map[string]interface{}{FormTriggerFormIDsKey: []interface{}{"f0", "f1"}}, "f1", true},
		{"a typed list", map[string]interface{}{FormTriggerFormIDsKey: []string{"f1"}}, "f1", true},
		{"a form not on the list", map[string]interface{}{FormTriggerFormIDsKey: []interface{}{"f0"}}, "f1", false},
		{"blank entries do not widen the list", map[string]interface{}{FormTriggerFormIDsKey: []interface{}{" ", "f0"}}, "f1", false},
		{"a list of blanks still means any form", map[string]interface{}{FormTriggerFormIDsKey: []interface{}{" "}}, "f1", true},
		{"an unreadable list refuses every form", map[string]interface{}{FormTriggerFormIDsKey: "f1"}, "f1", false},
		{"no form id refuses a named list", map[string]interface{}{FormTriggerFormIDsKey: []interface{}{"f1"}}, "", false},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if got := FormTriggerAccepts(tc.config, tc.formID); got != tc.want {
				t.Fatalf("FormTriggerAccepts = %v, want %v", got, tc.want)
			}
		})
	}
}

func TestFormUseNamesHowAWorkflowUsesAForm(t *testing.T) {
	trigger := func(cfg map[string]interface{}) Node {
		return Node{ID: "t", Type: NodeTypeTriggerFormSubmitted, Config: cfg}
	}
	sendForm := func(formID string) Node {
		return Node{ID: "s", Type: NodeTypeActionSendForm, Config: map[string]interface{}{FormNodeFormIDKey: formID}}
	}
	cases := []struct {
		name  string
		nodes []Node
		want  FormUse
		ok    bool
	}{
		{"a trigger on the form", []Node{trigger(map[string]interface{}{FormTriggerFormIDsKey: []interface{}{"f1"}})}, FormUseTrigger, true},
		{"a trigger on any form", []Node{trigger(nil)}, FormUseTrigger, true},
		{"a trigger on another form", []Node{trigger(map[string]interface{}{FormTriggerFormIDsKey: []interface{}{"f2"}})}, "", false},
		{"a node sending the form", []Node{{ID: "t", Type: NodeTypeTriggerMessageReceived}, sendForm("f1")}, FormUseNode, true},
		{"a node sending another form", []Node{{ID: "t", Type: NodeTypeTriggerMessageReceived}, sendForm("f2")}, "", false},
		{"the trigger wins over a node", []Node{trigger(nil), sendForm("f1")}, FormUseTrigger, true},
		{"no form at all", []Node{{ID: "t", Type: NodeTypeTriggerMessageReceived}}, "", false},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			w := &Workflow{Graph: Graph{Nodes: tc.nodes}}
			got, ok := w.FormUseOf("f1")
			if got != tc.want || ok != tc.ok {
				t.Fatalf("FormUseOf = %q, %v; want %q, %v", got, ok, tc.want, tc.ok)
			}
		})
	}
}
