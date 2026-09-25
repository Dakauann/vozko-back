package scheduled_message_repository

import (
	"reflect"
	"testing"

	sm "vozko/domain/scheduled_message"
	"vozko/domain/shared"
	"vozko/infra/database/schema"
)

func TestATemplateMessageSurvivesTheRoundTrip(t *testing.T) {
	in := &sm.ScheduledMessage{
		ID:              "sched-1",
		WorkspaceID:     "ws-1",
		EntryID:         "entry-1",
		EntryType:       shared.EntryTypeWhatsApp,
		CreatedByUserID: "user-1",
		Kind:            sm.KindTemplate,
		Template: &sm.TemplateContent{
			ID:           "tpl-1",
			Name:         "follow_up",
			Preview:      "Oi Ana, tudo certo?",
			BodyParams:   []string{"Ana", "amanhã"},
			HeaderParams: []string{"42"},
		},
		ScheduledAt: claimedAt,
		Status:      sm.StatusPending,
	}

	row, err := fromDomain(in)
	if err != nil {
		t.Fatalf("fromDomain: %v", err)
	}
	out := toDomain(&row)

	if out.Kind != sm.KindTemplate {
		t.Errorf("kind = %q", out.Kind)
	}
	if !reflect.DeepEqual(out.Template, in.Template) {
		t.Errorf("template = %+v, want %+v", out.Template, in.Template)
	}
}

func TestATextMessageHasNoTemplate(t *testing.T) {
	in := &sm.ScheduledMessage{ID: "sched-1", Kind: sm.KindText, Text: "oi", EntryType: shared.EntryTypeWhatsApp}

	row, err := fromDomain(in)
	if err != nil {
		t.Fatalf("fromDomain: %v", err)
	}
	if row.TemplateID != nil || row.TemplateBodyParams != nil || row.TemplateHeaderParams != nil {
		t.Fatalf("a text row carries template columns: %+v", row)
	}
	out := toDomain(&row)
	if out.Kind != sm.KindText || out.Template != nil {
		t.Fatalf("out = %+v", out)
	}
}

func TestAnUnreadableTemplateRowKeepsItsTemplateButNoValues(t *testing.T) {
	id := "tpl-1"
	row := schema.ScheduledMessage{
		ID:                 "sched-1",
		EntryType:          string(shared.EntryTypeWhatsApp),
		Kind:               string(sm.KindTemplate),
		TemplateID:         &id,
		TemplateBodyParams: []byte("not json"),
	}

	out := toDomain(&row)
	if out.Template == nil || out.Template.ID != "tpl-1" {
		t.Fatalf("template = %+v, want the template kept so the send is refused, not turned into text", out.Template)
	}
	if out.Template.BodyParams != nil {
		t.Errorf("body params = %v, want none so the send rules refuse the missing values", out.Template.BodyParams)
	}
}
