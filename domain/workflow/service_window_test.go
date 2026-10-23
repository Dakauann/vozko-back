package workflow

import "testing"

func TestEachTriggerStartsWithTheServiceWindowItsChannelReallyHas(t *testing.T) {
	cases := []struct {
		name    string
		trigger TriggerType
		want    ServiceWindow
	}{
		{"a received message opens the window", TriggerMessageReceived, ServiceWindowOpen},
		{"a first message opens the window", TriggerFirstMessage, ServiceWindowOpen},
		{"an agent is called while it talks to the customer", TriggerAgentCall, ServiceWindowOpen},
		{"a form is answered by the customer a moment ago", TriggerFormSubmitted, ServiceWindowOpen},
		{"a webhook comes from another system, the customer said nothing", TriggerWebhook, ServiceWindowClosed},
		{"a birthday starts on a lead nobody talked to", TriggerBirthday, ServiceWindowClosed},
		{"a stage change is an operator action, not a customer message", TriggerStageAdded, ServiceWindowClosed},
		{"an unknown trigger is treated as closed", TriggerType("trigger_unknown"), ServiceWindowClosed},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got := DefaultServiceWindow(tc.trigger)
			if got == nil || *got != tc.want {
				t.Fatalf("window = %v, want %s", got, tc.want)
			}
		})
	}
}

func TestAVoiceCallHasNoServiceWindow(t *testing.T) {
	if got := DefaultServiceWindow(TriggerCallReceived); got != nil {
		t.Fatalf("window = %s, want none for a call", *got)
	}
}

func TestTheTriggerSpecCarriesItsDefaultWindow(t *testing.T) {
	webhook := onlySpec(t, triggerGraph(Node{ID: "t", Type: NodeTypeTriggerWebhook}), TriggerInputSources{})
	if webhook.Window == nil || *webhook.Window != ServiceWindowClosed {
		t.Fatalf("webhook window = %v", webhook.Window)
	}
	message := onlySpec(t, triggerGraph(Node{ID: "t", Type: NodeTypeTriggerMessageReceived}), TriggerInputSources{})
	if message.Window == nil || *message.Window != ServiceWindowOpen {
		t.Fatalf("message window = %v", message.Window)
	}
	voice := onlySpec(t, &Graph{Nodes: []Node{{ID: "t", Type: NodeTypeTriggerCallReceived}}}, TriggerInputSources{})
	if voice.Window != nil {
		t.Fatalf("voice window = %s, want null", *voice.Window)
	}
}

func TestTheTesterWindowFollowsTheChoiceElseTheSpecDefault(t *testing.T) {
	open, closed := true, false
	webhook := onlySpec(t, triggerGraph(Node{ID: "t", Type: NodeTypeTriggerWebhook}), TriggerInputSources{})
	message := onlySpec(t, triggerGraph(Node{ID: "t", Type: NodeTypeTriggerMessageReceived}), TriggerInputSources{})
	voice := onlySpec(t, &Graph{Nodes: []Node{{ID: "t", Type: NodeTypeTriggerCallReceived}}}, TriggerInputSources{})
	cases := []struct {
		name   string
		spec   TriggerInputSpec
		chosen *bool
		want   bool
	}{
		{"no choice keeps the closed default of a webhook", webhook, nil, false},
		{"no choice keeps the open default of a message", message, nil, true},
		{"the tester can open the window of a webhook", webhook, &open, true},
		{"the tester can close the window of a webhook it opened", webhook, &closed, false},
		{"a message trigger ignores the choice because the customer's message opened the window", message, &closed, true},
		{"a trigger without a window never claims an open one", voice, &open, false},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if got := tc.spec.WindowOpen(tc.chosen); got != tc.want {
				t.Fatalf("open = %v, want %v", got, tc.want)
			}
		})
	}
}

func TestOnlyTriggersWhoseWindowIsNotOpenedByTheCustomersOwnMessageLetTheTesterChooseIt(t *testing.T) {
	cases := []struct {
		name     string
		node     Node
		editable bool
	}{
		{"a received message opens the window itself", Node{ID: "t", Type: NodeTypeTriggerMessageReceived}, false},
		{"a first message opens the window itself", Node{ID: "t", Type: NodeTypeTriggerFirstMessage}, false},
		{"a webhook may come with or without a recent customer message", Node{ID: "t", Type: NodeTypeTriggerWebhook}, true},
		{"a birthday may find a recent conversation", Node{ID: "t", Type: NodeTypeTriggerBirthday}, true},
		{"a stage change may happen mid conversation", Node{ID: "t", Type: NodeTypeTriggerStageAdded}, true},
		{"a form answer may be old", Node{ID: "t", Type: NodeTypeTriggerFormSubmitted}, true},
		{"an agent call may come after the window closed", Node{ID: "t", Type: NodeTypeTriggerAgentCall}, true},
		{"a voice call has no window to choose", Node{ID: "t", Type: NodeTypeTriggerCallReceived}, false},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			spec := onlySpec(t, &Graph{Nodes: []Node{tc.node}}, TriggerInputSources{})
			if spec.WindowEditable != tc.editable {
				t.Fatalf("windowEditable = %v, want %v", spec.WindowEditable, tc.editable)
			}
		})
	}
}
