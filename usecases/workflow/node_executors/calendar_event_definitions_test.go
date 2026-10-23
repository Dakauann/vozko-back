package node_executors

import (
	"strings"
	"testing"

	"vozko/domain/workflow"
)

func TestTheCalendarNodesAreNamedForTheEventTheyWriteNotForAMeeting(t *testing.T) {
	cases := []struct {
		def       workflow.NodeDefinition
		wantType  workflow.NodeType
		wantLabel string
	}{
		{NewCreateCalendarEventExecutor(nil, nil, nil).Definition(), workflow.NodeTypeActionCreateCalendarEvent, "Criar evento na agenda"},
		{NewRescheduleCalendarEventExecutor(nil).Definition(), workflow.NodeTypeActionRescheduleCalendarEvent, "Reagendar evento na agenda"},
	}
	for _, c := range cases {
		t.Run(c.wantLabel, func(t *testing.T) {
			if c.def.Type != c.wantType || c.def.Label != c.wantLabel {
				t.Fatalf("definition = %q %q, want %q %q", c.def.Type, c.def.Label, c.wantType, c.wantLabel)
			}
			copy := []string{c.def.Label, c.def.Description, c.def.Guidance.When, c.def.Guidance.Behavior}
			for _, field := range c.def.ConfigSchema {
				copy = append(copy, field.Label, field.Placeholder)
			}
			for key, value := range c.def.DefaultConfig {
				if text, isText := value.(string); isText && key != "start_time" && key != "event_id" {
					copy = append(copy, text)
				}
			}
			for _, text := range copy {
				if strings.Contains(strings.ToLower(text), "reuni") {
					t.Errorf("%q still names the node a meeting", text)
				}
			}
			if !strings.Contains(c.def.Description, "evento") {
				t.Errorf("description %q must say evento", c.def.Description)
			}
		})
	}
}

func TestTheMeetLinkIsOnlyAnOptionOfTheCreatedEvent(t *testing.T) {
	def := NewCreateCalendarEventExecutor(nil, nil, nil).Definition()
	var meetField *workflow.ConfigField
	for i := range def.ConfigSchema {
		if def.ConfigSchema[i].Key == "create_google_meet" {
			meetField = &def.ConfigSchema[i]
		}
	}
	if meetField == nil || meetField.Required {
		t.Fatalf("create_google_meet = %+v, want an optional field", meetField)
	}
	if !strings.Contains(def.Description, "Google Meet") || !strings.Contains(def.Description, "opcional") {
		t.Fatalf("description %q must present the Meet link as optional", def.Description)
	}
	for _, key := range def.OutputKeys {
		if key.Key == "meeting_link" && !strings.Contains(key.Description, "Google Meet") {
			t.Fatalf("meeting_link must stay the Meet link output, got %q", key.Description)
		}
	}
}
