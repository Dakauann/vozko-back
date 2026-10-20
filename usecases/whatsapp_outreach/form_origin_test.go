package whatsapp_outreach

import (
	"context"
	"testing"

	wf "vozko/domain/whatsappform"
)

func TestStartConversation_TellsTheFormSendWhereItCameFrom(t *testing.T) {
	uc := newUC(t)

	result, err := uc.uc.Execute(context.Background(), input())
	if err != nil {
		t.Fatalf("start: %v", err)
	}
	in := uc.sender.lastIn
	if in.Origin != wf.OriginTemplateStart || in.LeadID == "" || in.LeadID != result.LeadID || in.EntryID != result.EntryID {
		t.Fatalf("billed send %+v, want origin %s with the lead %s and entry %s", in, wf.OriginTemplateStart, result.LeadID, result.EntryID)
	}
}

func TestConversationTemplate_TellsTheFormSendWhereItCameFrom(t *testing.T) {
	cases := []struct {
		name      string
		origin    wf.SendOrigin
		originRef string
	}{
		{name: "scheduled message", origin: wf.OriginTemplateScheduled, originRef: "sched-1"},
		{name: "conversation template", origin: wf.OriginTemplate},
		{name: "caller that names no origin"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			f := newConversationUC(t)
			in := conversationInput()
			in.Origin, in.OriginRef = tc.origin, tc.originRef

			if _, err := f.uc.Send(context.Background(), in); err != nil {
				t.Fatalf("send: %v", err)
			}
			got := f.sender.lastIn
			if got.Origin != tc.origin || got.OriginRef != tc.originRef || got.LeadID != "lead-1" || got.EntryID != "entry-1" {
				t.Fatalf("billed send %+v, want origin %q ref %q for lead-1 on entry-1", got, tc.origin, tc.originRef)
			}
		})
	}
}
