package unofficial_whatsapp_campaign

import (
	"context"
	"errors"
	"testing"

	uw "vozko/domain/unofficial_whatsapp"
	uwc "vozko/domain/unofficial_whatsapp_campaign"
)

type fakeGroupSeats struct {
	byInstance map[string]map[string]*uw.Group
}

func (f fakeGroupSeats) FindByJID(_ context.Context, instanceID, jid string) (*uw.Group, error) {
	if g, ok := f.byInstance[instanceID][jid]; ok {
		return g, nil
	}
	return nil, uw.ErrGroupNotFound
}

func seatsOfInst1() fakeGroupSeats {
	return fakeGroupSeats{byInstance: map[string]map[string]*uw.Group{
		"inst-1": {
			"120363000000000001@g.us": {InstanceID: "inst-1", JID: "120363000000000001@g.us", Subject: "Ofertas VIP #1"},
			"120363000000000002@g.us": {InstanceID: "inst-1", JID: "120363000000000002@g.us", Subject: "Ofertas VIP #2", Announce: true, WeAreAdmin: true},
			"120363000000000003@g.us": {InstanceID: "inst-1", JID: "120363000000000003@g.us", Subject: "Avisos", Announce: true},
		},
	}}
}

func groupDraft(targets ...uwc.TargetInput) *uwc.Campaign {
	return &uwc.Campaign{
		WorkspaceID: "ws-1", InstanceID: "inst-1", Name: "Ofertas da semana", Audience: uwc.AudienceGroups,
		Message: uwc.MessageSpec{Kind: uwc.KindText, Bodies: []string{"Oferta de hoje: café 500 g por R$ 14,90."}},
		Targets: targets,
	}
}

func TestCreateWithGroupTargetsMakesOneEntryPerGroupOfTheNumber(t *testing.T) {
	h := newLeadTargetHarness()
	h.uc.SetGroupSeats(seatsOfInst1())

	created, err := h.uc.Execute(context.Background(), groupDraft(
		uwc.TargetInput{GroupJID: "120363000000000001@g.us"},
		uwc.TargetInput{GroupJID: "120363000000000002@g.us"},
	), uw.Unrestricted())
	if err != nil {
		t.Fatalf("Execute: %v", err)
	}
	if created.Audience != uwc.AudienceGroups {
		t.Fatalf("audience = %q, want groups", created.Audience)
	}

	h.entries.mu.Lock()
	defer h.entries.mu.Unlock()
	if len(h.entries.entries) != 2 {
		t.Fatalf("entries = %d, want 2", len(h.entries.entries))
	}
	names := map[string]string{}
	for _, e := range h.entries.entries {
		if !e.TargetsGroup() || e.LeadID != "" || e.Number != "" {
			t.Fatalf("entry %+v does not target a group", e)
		}
		names[e.JID] = e.Name
	}
	if names["120363000000000001@g.us"] != "Ofertas VIP #1" {
		t.Fatalf("entry names = %v, want the group subjects", names)
	}
	if len(h.leads.calls) != 0 || len(h.numbers.byNum) != 0 {
		t.Fatalf("a group campaign resolved or created leads")
	}
}

func TestCreateRefusesAGroupTheNumberIsNotIn(t *testing.T) {
	h := newLeadTargetHarness()
	h.uc.SetGroupSeats(seatsOfInst1())

	_, err := h.uc.Execute(context.Background(), groupDraft(
		uwc.TargetInput{GroupJID: "120363999999999999@g.us"},
	), uw.Unrestricted())
	if !errors.Is(err, uwc.ErrCampaignTargetInvalid) {
		t.Fatalf("Execute error = %v, want ErrCampaignTargetInvalid", err)
	}
	if len(h.campaigns.campaigns) != 0 {
		t.Fatalf("a campaign was saved with a foreign group")
	}
}

func TestCreateWithGroupTargetsFailsClosedWithoutTheGroupDirectory(t *testing.T) {
	h := newLeadTargetHarness()
	_, err := h.uc.Execute(context.Background(), groupDraft(
		uwc.TargetInput{GroupJID: "120363000000000001@g.us"},
	), uw.Unrestricted())
	if !errors.Is(err, ErrGroupTargetsUnavailable) {
		t.Fatalf("Execute error = %v, want ErrGroupTargetsUnavailable", err)
	}
}

func TestCreateWithGroupTargetsRefusesPersonalPlaceholders(t *testing.T) {
	h := newLeadTargetHarness()
	h.uc.SetGroupSeats(seatsOfInst1())
	draft := groupDraft(uwc.TargetInput{GroupJID: "120363000000000001@g.us"})
	draft.Message.Bodies = []string{"Olá {{1}}, oferta de hoje"}

	_, err := h.uc.Execute(context.Background(), draft, uw.Unrestricted())
	if !errors.Is(err, uwc.ErrCampaignVariablesMismatch) {
		t.Fatalf("Execute error = %v, want ErrCampaignVariablesMismatch", err)
	}
}

func TestCreateRefusesAGroupWhereOnlyAdminsPostAndTheNumberIsNotAdmin(t *testing.T) {
	h := newLeadTargetHarness()
	h.uc.SetGroupSeats(seatsOfInst1())

	_, err := h.uc.Execute(context.Background(), groupDraft(uwc.TargetInput{GroupJID: "120363000000000003@g.us"}), uw.Unrestricted())
	if !errors.Is(err, uwc.ErrCampaignTargetInvalid) {
		t.Fatalf("Execute = %v, want the read-only group refused", err)
	}
}
