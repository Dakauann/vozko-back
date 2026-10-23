package campaignaudience_usecase

import (
	"context"
	"errors"
	"reflect"
	"testing"

	"vozko/domain/campaign"
	"vozko/domain/campaignaudience"
	uw "vozko/domain/unofficial_whatsapp"
	uwc "vozko/domain/unofficial_whatsapp_campaign"
	wc "vozko/domain/whatsapp_campaign"
)

type recordingOfficial struct{ got []*wc.Campaign }

func (r *recordingOfficial) Execute(_ context.Context, c *wc.Campaign) (*wc.Campaign, error) {
	copied := *c
	r.got = append(r.got, &copied)
	return &copied, nil
}

type recordingUnofficial struct{ got []*uwc.Campaign }

func (r *recordingUnofficial) Execute(_ context.Context, c *uwc.Campaign, _ uw.DepartmentScope) (*uwc.Campaign, error) {
	copied := *c
	r.got = append(r.got, &copied)
	return &copied, nil
}

func TestOfficialCreateWithoutRecipientsIsTheCreateOfToday(t *testing.T) {
	w := newWorld(t)
	inner := &recordingOfficial{}
	create := OfficialCreate(inner, w.service)
	input := &wc.Campaign{WorkspaceID: "ws-1", TemplateID: "tpl", PhoneInputs: []wc.PhoneInput{{Number: "5511900010142", Name: "Maria", Variables: []string{"M"}, Metadata: map[string]any{"serie": "1"}}}}
	want := *input
	if _, err := create.Execute(ctx, input); err != nil {
		t.Fatal(err)
	}
	if len(inner.got) != 1 || !reflect.DeepEqual(*inner.got[0], want) {
		t.Fatalf("the phoneNumbers payload reached the create changed: %+v", inner.got)
	}
	if w.gate.acquired != 0 || w.selections.resolved != 0 {
		t.Fatal("a create without recipients read the audience")
	}
}

func TestOfficialCreateTurnsAListIntoTheSamePhoneInputsThePayloadMade(t *testing.T) {
	w := newWorld(t)
	w.channel.slots = []campaignaudience.Slot{{Key: "1"}}
	w.channel.judge = fakeJudge{}
	preview := w.upload(t, campaignaudience.ChannelWhatsApp, "telefone;nome;var1;Série\n(11) 90001-0142;Maria;M;1º ano\n", View{})
	inner := &recordingOfficial{}
	input := &wc.Campaign{WorkspaceID: "ws-1", TemplateID: "tpl", BusinessPhoneID: "phone-1", Recipients: &campaignaudience.Request{Spec: campaignaudience.Spec{Source: campaignaudience.SourceList, ListID: preview.ID}, UserID: "user-1"}}
	if _, err := OfficialCreate(inner, w.service).Execute(ctx, input); err != nil {
		t.Fatal(err)
	}
	want := []wc.PhoneInput{{Number: "5511900010142", Name: "Maria", Variables: []string{"M"}, Metadata: map[string]any{"serie": "1º ano"}}}
	if got := inner.got[0]; !reflect.DeepEqual(got.PhoneInputs, want) || got.Recipients != nil {
		t.Fatalf("create got %+v", got)
	}
}

func TestOfficialCreateRefusesRecipientsWithPhoneNumbersOrWithoutTheCreator(t *testing.T) {
	w := newWorld(t)
	inner := &recordingOfficial{}
	both := &wc.Campaign{WorkspaceID: "ws-1", PhoneInputs: []wc.PhoneInput{{Number: "5511900010142"}}, Recipients: &campaignaudience.Request{UserID: "user-1"}}
	if _, err := OfficialCreate(inner, w.service).Execute(ctx, both); !errors.Is(err, campaignaudience.ErrSourceAmbiguous) {
		t.Fatalf("both = %v", err)
	}
	anonymous := &wc.Campaign{WorkspaceID: "ws-1", Recipients: &campaignaudience.Request{Spec: campaignaudience.Spec{Source: campaignaudience.SourceList, ListID: "x"}}}
	if _, err := OfficialCreate(inner, w.service).Execute(ctx, anonymous); !errors.Is(err, campaign.ErrCreationScopeMissing) {
		t.Fatalf("anonymous = %v", err)
	}
	if len(inner.got) != 0 {
		t.Fatal("a refused create reached the inner create")
	}
}

func TestOfficialCreateFromLeadsCarriesTheirVariablesAndSkips(t *testing.T) {
	w := newWorld(t)
	w.selections.ids = []string{"a", "b"}
	w.channel.slots = []campaignaudience.Slot{{Key: "1", Suggested: campaign.BindFirstName}}
	w.channel.judge = fakeJudge{leads: map[string]campaignaudience.Candidate{"a": {HasContact: true, Values: []string{"Ana"}}, "b": {HasContact: true, Values: []string{""}}}}
	spec := campaignaudience.Spec{Source: campaignaudience.SourceLeads, Selection: confirmed(everyoneSelection(), 2), Variables: []campaignaudience.Variable{{Key: "1", Missing: campaignaudience.MissingExclude}}}
	inner := &recordingOfficial{}
	if _, err := OfficialCreate(inner, w.service).Execute(ctx, &wc.Campaign{WorkspaceID: "ws-1", TemplateID: "tpl", Recipients: &campaignaudience.Request{Spec: spec, UserID: "user-1"}}); err != nil {
		t.Fatal(err)
	}
	want := []wc.PhoneInput{
		{LeadID: "a", Variables: []string{"Ana"}},
		{LeadID: "b", Skip: campaign.SkipMissingVariable, Missing: []campaign.MissingVariable{{Slot: 1, Source: campaign.BindFirstName}}},
	}
	if !reflect.DeepEqual(inner.got[0].PhoneInputs, want) {
		t.Fatalf("inputs = %+v", inner.got[0].PhoneInputs)
	}
}

func TestUnofficialCreateTakesRecipientsButNeverForGroups(t *testing.T) {
	w := newWorld(t)
	unofficial := &fakeChannel{name: campaignaudience.ChannelUnofficial, slots: []campaignaudience.Slot{{Key: "1"}}, judge: fakeJudge{}}
	w.service.AddChannel(unofficial)
	preview := w.upload(t, campaignaudience.ChannelUnofficial, "numero;nome;variavel1\n5511900010142;Maria;Maria\n", View{})
	inner := &recordingUnofficial{}
	create := UnofficialCreate(inner, w.service)
	in := &uwc.Campaign{WorkspaceID: "ws-1", InstanceID: "inst-1", Message: uwc.MessageSpec{Kind: uwc.KindText, Bodies: []string{"Oi {{1}}"}}, Recipients: &campaignaudience.Request{Spec: campaignaudience.Spec{Source: campaignaudience.SourceList, ListID: preview.ID}, UserID: "user-1"}}
	if _, err := create.Execute(ctx, in, uw.DepartmentScope{}); err != nil {
		t.Fatal(err)
	}
	want := []uwc.TargetInput{{Number: "5511900010142", Name: "Maria", Variables: []string{"Maria"}}}
	if !reflect.DeepEqual(inner.got[0].Targets, want) || inner.got[0].Recipients != nil {
		t.Fatalf("targets = %+v", inner.got[0].Targets)
	}
	groups := &uwc.Campaign{WorkspaceID: "ws-1", Audience: uwc.AudienceGroups, Recipients: &campaignaudience.Request{UserID: "user-1"}}
	if _, err := create.Execute(ctx, groups, uw.DepartmentScope{}); !errors.Is(err, campaignaudience.ErrSourceUnsupported) {
		t.Fatalf("groups = %v", err)
	}
	both := &uwc.Campaign{WorkspaceID: "ws-1", Targets: []uwc.TargetInput{{Number: "5511900010142"}}, Recipients: &campaignaudience.Request{UserID: "user-1"}}
	if _, err := create.Execute(ctx, both, uw.DepartmentScope{}); !errors.Is(err, campaignaudience.ErrSourceAmbiguous) {
		t.Fatalf("both = %v", err)
	}
	plain := &uwc.Campaign{WorkspaceID: "ws-1", Targets: []uwc.TargetInput{{Number: "5511900010142"}}}
	if _, err := create.Execute(ctx, plain, uw.DepartmentScope{}); err != nil || len(inner.got) != 2 || !reflect.DeepEqual(inner.got[1].Targets, plain.Targets) {
		t.Fatalf("a targets payload is unchanged: %+v, %v", inner.got, err)
	}
}
