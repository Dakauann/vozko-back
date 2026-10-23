package campaignaudience_usecase

import (
	"errors"
	"testing"

	"vozko/domain/campaign"
	"vozko/domain/campaignaudience"
	uwc "vozko/domain/unofficial_whatsapp_campaign"
	wc "vozko/domain/whatsapp_campaign"
)

func TestOfficialCounterCountsTheListTheCreateWouldUse(t *testing.T) {
	w := newWorld(t)
	w.channel.slots = []campaignaudience.Slot{{Key: "1"}}
	w.channel.judge = fakeJudge{}
	preview := w.upload(t, campaignaudience.ChannelWhatsApp, "telefone;nome;var1\n(11) 90001-0142;Maria;M\n(11) 90001-0143;Ana;A\n", View{})
	draft := &wc.Campaign{WorkspaceID: "ws-1", TemplateID: "tpl", BusinessPhoneID: "phone-1", Recipients: &campaignaudience.Request{Spec: campaignaudience.Spec{Source: campaignaudience.SourceList, ListID: preview.ID}, UserID: "user-1"}}
	count, err := NewOfficialCounter(w.service).Count(ctx, draft)
	if err != nil {
		t.Fatal(err)
	}
	if count.Sendable == nil || *count.Sendable != 2 {
		t.Fatalf("count %+v", count)
	}
}

func TestOfficialCounterCountsPhoneNumbersWithoutTheAudience(t *testing.T) {
	w := newWorld(t)
	draft := &wc.Campaign{WorkspaceID: "ws-1", PhoneInputs: []wc.PhoneInput{{Number: "5511900010142"}, {Number: "5511900010143"}}}
	count, err := NewOfficialCounter(w.service).Count(ctx, draft)
	if err != nil || *count.Sendable != 2 {
		t.Fatalf("count %+v %v", count, err)
	}
	if w.gate.acquired != 0 {
		t.Fatal("phone numbers read the audience")
	}
	empty, err := NewOfficialCounter(w.service).Count(ctx, &wc.Campaign{WorkspaceID: "ws-1"})
	if err != nil || *empty.Sendable != 0 {
		t.Fatalf("empty %+v %v", empty, err)
	}
}

func TestOfficialCounterRefusesWhatTheCreateRefuses(t *testing.T) {
	w := newWorld(t)
	both := &wc.Campaign{WorkspaceID: "ws-1", PhoneInputs: []wc.PhoneInput{{Number: "5511900010142"}}, Recipients: &campaignaudience.Request{UserID: "user-1"}}
	if _, err := NewOfficialCounter(w.service).Count(ctx, both); !errors.Is(err, campaignaudience.ErrSourceAmbiguous) {
		t.Fatalf("both = %v", err)
	}
	anonymous := &wc.Campaign{WorkspaceID: "ws-1", Recipients: &campaignaudience.Request{Spec: campaignaudience.Spec{Source: campaignaudience.SourceList, ListID: "x"}}}
	if _, err := NewOfficialCounter(w.service).Count(ctx, anonymous); !errors.Is(err, campaign.ErrCreationScopeMissing) {
		t.Fatalf("anonymous = %v", err)
	}
	if _, err := NewOfficialCounter(nil).Count(ctx, &wc.Campaign{WorkspaceID: "ws-1", Recipients: &campaignaudience.Request{UserID: "user-1"}}); !errors.Is(err, campaignaudience.ErrUnavailable) {
		t.Fatalf("unwired = %v", err)
	}
}

func TestUnofficialCounterRefusesGroupsAndCountsTargets(t *testing.T) {
	w := newWorld(t)
	groups := &uwc.Campaign{WorkspaceID: "ws-1", Audience: uwc.AudienceGroups, Recipients: &campaignaudience.Request{UserID: "user-1"}}
	if _, err := NewUnofficialCounter(w.service).Count(ctx, groups); !errors.Is(err, campaignaudience.ErrSourceUnsupported) {
		t.Fatalf("groups = %v", err)
	}
	listed := &uwc.Campaign{WorkspaceID: "ws-1", Targets: []uwc.TargetInput{{Number: "5511900010142"}}}
	count, err := NewUnofficialCounter(w.service).Count(ctx, listed)
	if err != nil || *count.Sendable != 1 {
		t.Fatalf("count %+v %v", count, err)
	}
}
