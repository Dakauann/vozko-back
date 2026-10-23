package scheduled_message_usecase

import (
	"reflect"
	"testing"

	sm "vozko/domain/scheduled_message"
)

func TestAScheduledTemplateKeepsItsHeaderValuesApartFromTheBody(t *testing.T) {
	in := templateSend(&sm.ScheduledMessage{ID: "m1", WorkspaceID: "ws-1", EntryID: "e1", Template: &sm.TemplateContent{ID: "t1", BodyParams: []string{"Ana"}, HeaderParams: []string{"Prisma"}}})
	if !reflect.DeepEqual(in.BodyParams, []string{"Ana"}) || !reflect.DeepEqual(in.HeaderParams, []string{"Prisma"}) || in.IdempotencyKey != "scheduled-message:m1" {
		t.Fatalf("input = %+v", in)
	}
}
