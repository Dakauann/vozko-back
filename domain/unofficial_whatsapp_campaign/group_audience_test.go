package unofficial_whatsapp_campaign

import (
	"errors"
	"testing"
	"time"

	"vozko/domain/campaign"
)

func groupCampaign(targets ...TargetInput) *Campaign {
	return &Campaign{
		WorkspaceID: "w1", Name: "Ofertas da semana",
		Audience: AudienceGroups,
		Message:  MessageSpec{Kind: KindText, Bodies: []string{"Oferta de hoje: café 500 g por R$ 14,90."}},
		Targets:  targets,
	}
}

func TestCampaignAudienceDefaultsToLeads(t *testing.T) {
	c := &Campaign{Name: "x", InstanceID: "n1"}
	c.Normalize()
	if c.Audience != AudienceLeads || c.TargetsGroups() {
		t.Fatalf("audience = %q, want leads by default", c.Audience)
	}
}

func TestGroupCampaignNeedsNoExitCommand(t *testing.T) {
	c := groupCampaign(TargetInput{GroupJID: "120363153742561022@g.us", Name: "Ofertas VIP #1"})
	c.Normalize()
	if err := c.Validate(); err != nil {
		t.Fatalf("group campaign without a /sair command refused: %v", err)
	}
}

func TestLeadCampaignStillNeedsItsExitCommand(t *testing.T) {
	c := groupCampaign(TargetInput{Number: "5511981124410"})
	c.Audience = AudienceLeads
	c.Normalize()
	if err := c.Validate(); err == nil {
		t.Fatalf("lead campaign without an exit command accepted")
	}
}

func TestGroupCampaignTargetsMustBeGroups(t *testing.T) {
	cases := map[string]TargetInput{
		"a phone number":  {Number: "5511981124410"},
		"a lead":          {LeadID: "lead-1"},
		"a contact jid":   {GroupJID: "5511981124410@s.whatsapp.net"},
		"group plus lead": {GroupJID: "120363@g.us", LeadID: "lead-1"},
	}
	for name, target := range cases {
		t.Run(name, func(t *testing.T) {
			c := groupCampaign(target)
			c.Normalize()
			if err := c.Validate(); !errors.Is(err, ErrCampaignTargetInvalid) {
				t.Fatalf("Validate = %v, want ErrCampaignTargetInvalid", err)
			}
		})
	}
}

func TestLeadCampaignRefusesGroupTargets(t *testing.T) {
	c := groupCampaign(TargetInput{Number: "5511981124410", GroupJID: "120363@g.us"})
	c.Audience = AudienceLeads
	c.InstanceID = "n1"
	c.Message.Bodies = []string{"Oi! Responda /sair para não receber mais mensagens."}
	c.Normalize()
	if err := c.Validate(); !errors.Is(err, ErrCampaignTargetInvalid) {
		t.Fatalf("Validate = %v, want a group target refused in a lead campaign", err)
	}
}

func TestGroupCampaignDropsRepeatedGroups(t *testing.T) {
	c := groupCampaign(
		TargetInput{GroupJID: " 120363@g.us "},
		TargetInput{GroupJID: "120363@g.us"},
		TargetInput{GroupJID: "120999@g.us"},
	)
	c.Normalize()
	if len(c.Targets) != 2 || c.Targets[0].GroupJID != "120363@g.us" {
		t.Fatalf("targets = %+v, want two distinct trimmed groups", c.Targets)
	}
}

func TestUnknownAudienceIsRefused(t *testing.T) {
	c := groupCampaign(TargetInput{GroupJID: "120363@g.us"})
	c.Normalize()
	c.Audience = "everyone"
	if err := c.Validate(); !errors.Is(err, ErrCampaignAudienceInvalid) {
		t.Fatalf("Validate = %v, want ErrCampaignAudienceInvalid", err)
	}
}

func TestGroupEntryNeedsNoLeadAndNoNumberCheck(t *testing.T) {
	now := time.Date(2026, 10, 17, 15, 0, 0, 0, time.UTC)
	e := NewGroupEntry("c1", "w1", "120363153742561022@g.us", "Ofertas VIP #1", now)
	if err := e.Validate(); err != nil {
		t.Fatalf("group entry refused: %v", err)
	}
	if !e.TargetsGroup() {
		t.Fatalf("group entry does not target a group")
	}
	if e.NeedsNumberCheck(now.Add(30 * 24 * time.Hour)) {
		t.Fatalf("a group entry asked for a phone number check")
	}
	if e.Status != campaign.SendStatusPending || e.Number != "" || e.LeadID != "" {
		t.Fatalf("group entry = %+v", e)
	}
}

func TestEntryWithoutLeadOrGroupIsRefused(t *testing.T) {
	e := Entry{CampaignID: "c1", Status: campaign.SendStatusPending, JID: "5511981124410@s.whatsapp.net"}
	if err := e.Validate(); !errors.Is(err, ErrEntryLeadRequired) {
		t.Fatalf("Validate = %v, want ErrEntryLeadRequired", err)
	}
	lead := Entry{CampaignID: "c1", LeadID: "l1", Status: campaign.SendStatusPending}
	if lead.TargetsGroup() {
		t.Fatalf("a lead entry reads as a group entry")
	}
}

func TestGroupCampaignEditNeedsNoExitCommand(t *testing.T) {
	existing := groupCampaign(TargetInput{GroupJID: "120363@g.us"})
	existing.Normalize()
	edit := groupCampaign()
	edit.Message.Bodies = []string{"Nova oferta: arroz 5 kg por R$ 24,90."}
	if err := existing.RequireExitFor(edit); err != nil {
		t.Fatalf("group campaign edit asked for an exit command: %v", err)
	}
}

func TestAnOlderGroupCampaignRefusesAnotherNumber(t *testing.T) {
	existing := groupCampaign(TargetInput{GroupJID: "120363@g.us"})
	existing.InstanceID = "n1"
	existing.Normalize()
	if err := existing.RefuseNumberChange(&Campaign{InstanceID: "n2"}); !errors.Is(err, ErrGroupCampaignTakesNoNumber) {
		t.Fatalf("RefuseNumberChange = %v, want ErrGroupCampaignTakesNoNumber", err)
	}
	if err := existing.RefuseNumberChange(&Campaign{InstanceID: "n1"}); err != nil {
		t.Fatalf("same number refused: %v", err)
	}
	if err := existing.RefuseNumberChange(&Campaign{}); err != nil {
		t.Fatalf("no number change refused: %v", err)
	}
	leads := &Campaign{InstanceID: "n1", Audience: AudienceLeads}
	if err := leads.RefuseNumberChange(&Campaign{InstanceID: "n2"}); err != nil {
		t.Fatalf("lead campaign number change refused: %v", err)
	}
}

func TestGroupCampaignRefusesAFunnelItCannotApply(t *testing.T) {
	c := groupCampaign(TargetInput{GroupJID: "120363153742561022@g.us", Name: "Ofertas VIP #1"})
	c.PipelineID = "funil-vendas"
	c.Normalize()
	if err := c.Validate(); !errors.Is(err, ErrGroupCampaignFunnel) {
		t.Fatalf("group campaign with a funnel = %v, want ErrGroupCampaignFunnel", err)
	}
}
