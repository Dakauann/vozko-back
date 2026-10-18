package buttonaction

import (
	"testing"
	"time"
)

func TestAdmittingACampaignOnAnotherTemplateDropsTheOldRoutes(t *testing.T) {
	ack := &Acknowledgement{By: "user-1", At: time.Date(2026, 10, 16, 12, 0, 0, 0, time.UTC)}
	oldRef, newRef := TemplateRef{Name: "promo_outubro", Language: "pt_BR"}, TemplateRef{Name: "promo_novembro", Language: "pt_BR"}
	stored := CampaignRoutes{
		WorkspaceID: "ws", CampaignID: "camp", Template: oldRef, Version: 3,
		Routes: []ButtonAction{{Template: oldRef, ButtonIndex: 0, ButtonText: "Falar com atendente", Kind: KindHandOff}},
	}
	cases := []struct {
		name      string
		current   CampaignRoutes
		admission CampaignAdmission
		write     bool
		routes    int
		ref       TemplateRef
		ack       bool
	}{
		{"a new template drops the routes", stored, CampaignAdmission{Template: newRef}, true, 0, newRef, false},
		{"a new template with an acknowledgement", stored, CampaignAdmission{Template: newRef, ExitRisk: ack}, true, 0, newRef, true},
		{"the same template keeps the routes", stored, CampaignAdmission{Template: oldRef}, false, 1, oldRef, false},
		{"the same template records the acknowledgement", stored, CampaignAdmission{Template: oldRef, ExitRisk: ack}, true, 1, oldRef, true},
		{"nothing stored and nothing to acknowledge", CampaignRoutes{WorkspaceID: "ws", CampaignID: "camp", Version: FirstVersion}, CampaignAdmission{Template: newRef}, false, 0, TemplateRef{}, false},
		{"nothing stored and an acknowledgement", CampaignRoutes{WorkspaceID: "ws", CampaignID: "camp", Version: FirstVersion}, CampaignAdmission{Template: newRef, ExitRisk: ack}, true, 0, newRef, true},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			next, write := tc.current.Admit(tc.admission)
			if write != tc.write || len(next.Routes) != tc.routes || next.Template != tc.ref || (next.ExitRisk != nil) != tc.ack {
				t.Fatalf("got %+v, write %v", next, write)
			}
			if tc.write && next.UpdatedBy == "" && tc.ack {
				t.Fatalf("the acknowledger must be the author: %+v", next)
			}
		})
	}
}
