package unofficial_whatsapp_campaign

import (
	"context"
	"errors"
	"reflect"
	"strings"
	"testing"

	uw "vozko/domain/unofficial_whatsapp"
	uwc "vozko/domain/unofficial_whatsapp_campaign"
)

const (
	vipOne  = "120363000000000001@g.us"
	vipTwo  = "120363000000000002@g.us"
	notices = "120363000000000003@g.us"
	foreign = "120363000000000004@g.us"
	quiet   = "120363000000000005@g.us"
)

type fakeSenderDirectory struct {
	instances []*uw.Instance
	seats     map[string][]*uw.Group
	err       error
}

func (f fakeSenderDirectory) SeatsOf(_ context.Context, workspaceID string, jids []string) (map[string][]*uw.Group, error) {
	if f.err != nil {
		return nil, f.err
	}
	out := map[string][]*uw.Group{}
	for _, jid := range jids {
		for _, seat := range f.seats[jid] {
			if seat.WorkspaceID == workspaceID {
				out[jid] = append(out[jid], seat)
			}
		}
	}
	return out, nil
}

func (f fakeSenderDirectory) WorkspaceInstances(_ context.Context, workspaceID string) ([]*uw.Instance, error) {
	if f.err != nil {
		return nil, f.err
	}
	var out []*uw.Instance
	for _, i := range f.instances {
		if i.WorkspaceID == workspaceID {
			out = append(out, i)
		}
	}
	return out, nil
}

func strp(s string) *string { return &s }

func groupSeat(instanceID, jid, subject string, announce, admin bool) *uw.Group {
	return &uw.Group{WorkspaceID: "ws-1", InstanceID: instanceID, JID: jid, Subject: subject, Announce: announce, WeAreAdmin: admin}
}

func salesNumbers() fakeSenderDirectory {
	return fakeSenderDirectory{
		instances: []*uw.Instance{
			{ID: "n1", WorkspaceID: "ws-1", Status: uw.StatusConnected, DepartmentID: strp("sales"), DisplayName: "Vendas 1"},
			{ID: "n2", WorkspaceID: "ws-1", Status: uw.StatusConnected, DepartmentID: strp("sales"), DisplayName: "Vendas 2"},
			{ID: "n3", WorkspaceID: "ws-1", Status: uw.StatusConnected, DepartmentID: strp("support"), DisplayName: "Suporte"},
			{ID: "n4", WorkspaceID: "ws-1", Status: uw.StatusBanned, DepartmentID: strp("sales"), DisplayName: "Banido"},
			{ID: "n5", WorkspaceID: "ws-1", Status: uw.StatusDisconnected, DepartmentID: strp("sales"), DisplayName: "Vendas 5"},
		},
		seats: map[string][]*uw.Group{
			vipOne:  {groupSeat("n1", vipOne, "Ofertas VIP #1", false, false), groupSeat("n3", vipOne, "Ofertas VIP #1", false, false)},
			vipTwo:  {groupSeat("n2", vipTwo, "Ofertas VIP #2", true, true), groupSeat("n1", vipTwo, "Ofertas VIP #2", true, false)},
			notices: {groupSeat("n1", notices, "Avisos", true, false)},
			foreign: {groupSeat("n3", foreign, "Suporte interno", false, false), groupSeat("n4", foreign, "Suporte interno", false, false)},
			quiet:   {groupSeat("n5", quiet, "Clientes antigos", false, false)},
		},
	}
}

func groupSalesScope() uw.DepartmentScope {
	return uw.DepartmentScope{Restrict: true, DepartmentIDs: []string{"sales"}}
}

func groupDraft(targets ...uwc.TargetInput) *uwc.Campaign {
	return &uwc.Campaign{
		WorkspaceID: "ws-1", Name: "Ofertas da semana", Audience: uwc.AudienceGroups,
		Message: uwc.MessageSpec{Kind: uwc.KindText, Bodies: []string{"Oferta de hoje: café 500 g por R$ 14,90."}},
		Targets: targets,
	}
}

func newGroupCreateHarness() *leadTargetHarness {
	h := newLeadTargetHarness()
	h.uc.SetGroupSenders(salesNumbers())
	return h
}

func TestCreateWithGroupTargetsTakesNoNumberAndKeepsTheNumbersTheCreatorMayUse(t *testing.T) {
	h := newGroupCreateHarness()

	created, err := h.uc.Execute(context.Background(), groupDraft(
		uwc.TargetInput{GroupJID: vipOne},
		uwc.TargetInput{GroupJID: vipTwo},
	), groupSalesScope())
	if err != nil {
		t.Fatalf("Execute: %v", err)
	}
	if created.Audience != uwc.AudienceGroups || created.InstanceID != "" {
		t.Fatalf("created = %+v, want a group campaign without a number", created)
	}
	if !reflect.DeepEqual(created.SenderInstanceIDs, []string{"n1", "n2", "n5"}) {
		t.Fatalf("sender pool = %v, want the creator's usable numbers only", created.SenderInstanceIDs)
	}

	h.entries.mu.Lock()
	defer h.entries.mu.Unlock()
	if len(h.entries.entries) != 2 {
		t.Fatalf("entries = %d, want 2", len(h.entries.entries))
	}
	names := map[string]string{}
	for _, e := range h.entries.entries {
		if !e.TargetsGroup() || e.LeadID != "" || e.Number != "" || e.SenderInstanceID != "" {
			t.Fatalf("entry %+v does not target a group or already names a sender", e)
		}
		names[e.JID] = e.Name
	}
	if names[vipOne] != "Ofertas VIP #1" || names[vipTwo] != "Ofertas VIP #2" {
		t.Fatalf("entry names = %v, want the group subjects", names)
	}
	if len(h.leads.calls) != 0 || len(h.numbers.byNum) != 0 {
		t.Fatalf("a group campaign resolved or created leads")
	}
}

func TestCreateRefusesAGroupNoneOfTheCreatorsNumbersIsIn(t *testing.T) {
	h := newGroupCreateHarness()

	_, err := h.uc.Execute(context.Background(), groupDraft(uwc.TargetInput{GroupJID: foreign}), groupSalesScope())

	if !errors.Is(err, uwc.ErrCampaignTargetInvalid) || !strings.Contains(err.Error(), "Suporte interno") {
		t.Fatalf("Execute error = %v, want the group named and refused", err)
	}
	if len(h.campaigns.campaigns) != 0 {
		t.Fatalf("a campaign was saved with a group no number may post in")
	}
}

func TestCreateRefusesAGroupWhereOnlyAdminsPostAndNoNumberThereIsAdmin(t *testing.T) {
	h := newGroupCreateHarness()

	_, err := h.uc.Execute(context.Background(), groupDraft(uwc.TargetInput{GroupJID: notices}), groupSalesScope())

	if !errors.Is(err, uwc.ErrCampaignTargetInvalid) || !strings.Contains(err.Error(), "Avisos") {
		t.Fatalf("Execute = %v, want the admins-only group refused by name", err)
	}
}

func TestCreateKeepsAGroupWhoseNumberIsOfflineForNow(t *testing.T) {
	h := newGroupCreateHarness()

	if _, err := h.uc.Execute(context.Background(), groupDraft(uwc.TargetInput{GroupJID: quiet}), groupSalesScope()); err != nil {
		t.Fatalf("Execute = %v, want an offline number to be waited for, not refused", err)
	}
}

func TestCreateRefusesAGroupCampaignThatPicksANumber(t *testing.T) {
	h := newGroupCreateHarness()
	draft := groupDraft(uwc.TargetInput{GroupJID: vipOne})
	draft.InstanceID = "n1"

	_, err := h.uc.Execute(context.Background(), draft, groupSalesScope())

	if !errors.Is(err, uwc.ErrGroupCampaignTakesNoNumber) {
		t.Fatalf("Execute = %v, want ErrGroupCampaignTakesNoNumber", err)
	}
}

func TestCreateWithGroupTargetsFailsClosedWithoutTheGroupDirectory(t *testing.T) {
	h := newLeadTargetHarness()
	_, err := h.uc.Execute(context.Background(), groupDraft(uwc.TargetInput{GroupJID: vipOne}), uw.Unrestricted())
	if !errors.Is(err, ErrGroupTargetsUnavailable) {
		t.Fatalf("Execute error = %v, want ErrGroupTargetsUnavailable", err)
	}
}

func TestCreateWithGroupTargetsRefusesPersonalPlaceholders(t *testing.T) {
	h := newGroupCreateHarness()
	draft := groupDraft(uwc.TargetInput{GroupJID: vipOne})
	draft.Message.Bodies = []string{"Olá {{1}}, oferta de hoje"}

	_, err := h.uc.Execute(context.Background(), draft, uw.Unrestricted())
	if !errors.Is(err, uwc.ErrCampaignVariablesMismatch) {
		t.Fatalf("Execute error = %v, want ErrCampaignVariablesMismatch", err)
	}
}

func TestCreateWithGroupTargetsIsRefusedWhenTheCreatorSeesNoNumber(t *testing.T) {
	h := newGroupCreateHarness()

	_, err := h.uc.Execute(context.Background(), groupDraft(uwc.TargetInput{GroupJID: vipOne}), uw.DepartmentScope{Restrict: true})

	if !errors.Is(err, uwc.ErrCampaignTargetInvalid) || len(h.campaigns.campaigns) != 0 {
		t.Fatalf("Execute = %v, want the group refused when no number is in scope", err)
	}
}
