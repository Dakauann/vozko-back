package campaignaudience_usecase

import (
	"errors"
	"reflect"
	"testing"

	"vozko/domain/campaign"
	"vozko/domain/campaignaudience"
	uw "vozko/domain/unofficial_whatsapp"
	uwc "vozko/domain/unofficial_whatsapp_campaign"
	wc "vozko/domain/whatsapp_campaign"
)

func needsClass(metadata map[string]any) []string {
	if text, _ := metadata["turma"].(string); text == "" {
		return []string{"turma"}
	}
	return nil
}

const classList = "telefone;nome;var1;Turma\n5511900010142;Maria;M;A\n5511900023301;João;J;\n5511900033301;Rita;R;\n"

func TestAListMissingTheMetadataTheAutomationNeedsIsReportedBeforeTheCreate(t *testing.T) {
	w := newWorld(t)
	w.channel.slots = []campaignaudience.Slot{{Key: "1"}}
	w.channel.judge = fakeJudge{}
	w.channel.metadata = needsClass
	view := View{Params: Params{TemplateID: "tpl", Automation: campaign.Automation{WorkflowID: "wf-1"}}}
	preview := w.upload(t, campaignaudience.ChannelWhatsApp, classList, view)
	want := []campaignaudience.MetadataGap{{Key: "turma", Missing: 2, Lines: []int{3, 4}}}
	if !reflect.DeepEqual(preview.MetadataMissing, want) {
		t.Fatalf("preview metadata = %+v", preview.MetadataMissing)
	}
	count, err := w.service.Count(ctx, testActor, campaignaudience.ChannelWhatsApp, view.Params, campaignaudience.Spec{Source: campaignaudience.SourceList, ListID: preview.ID})
	if err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(count.MetadataMissing, want) || !reflect.DeepEqual(count.Blockers, []campaignaudience.Blocker{campaignaudience.BlockerListMetadataMissing}) {
		t.Fatalf("count metadata = %+v blockers %v", count.MetadataMissing, count.Blockers)
	}
	if got := w.channel.params[len(w.channel.params)-1].Automation; got.WorkflowID != "wf-1" {
		t.Fatalf("the channel got automation %+v", got)
	}
	page, err := w.service.Rows(ctx, testActor, campaignaudience.ChannelWhatsApp, preview.ID, RowsQuery{View: view, MissingOnly: true, Page: 1, Limit: 50})
	if err != nil {
		t.Fatal(err)
	}
	if page.Total != 2 || page.Items[0].Name != "João" || !reflect.DeepEqual(page.Items[0].MetadataMissing, []string{"turma"}) || len(page.Items[0].Missing) != 0 {
		t.Fatalf("rows missing metadata = %+v", page)
	}
	if _, err := w.service.Recipients(ctx, testActor, campaignaudience.ChannelWhatsApp, view.Params, campaignaudience.Spec{Source: campaignaudience.SourceList, ListID: preview.ID}); !errors.Is(err, campaignaudience.ErrListMetadataMissing) {
		t.Fatalf("recipients = %v", err)
	}
}

func TestLeadsCannotFeedAnAutomationThatNeedsMetadata(t *testing.T) {
	w := newWorld(t)
	w.selections.ids = []string{"a"}
	w.channel.slots = []campaignaudience.Slot{{Key: "1", Suggested: campaign.BindFirstName}}
	w.channel.judge = fakeJudge{leads: map[string]campaignaudience.Candidate{"a": {HasContact: true, Values: []string{"Ana"}}}}
	w.channel.metadata = needsClass
	spec := campaignaudience.Spec{Source: campaignaudience.SourceLeads, Selection: confirmed(everyoneSelection(), 1)}
	count, err := w.service.Count(ctx, testActor, campaignaudience.ChannelWhatsApp, Params{}, spec)
	if err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(count.Blockers, []campaignaudience.Blocker{campaignaudience.BlockerLeadsMetadataUnavailable}) {
		t.Fatalf("blockers = %v", count.Blockers)
	}
	if _, err := w.service.Recipients(ctx, testActor, campaignaudience.ChannelWhatsApp, Params{}, spec); !errors.Is(err, campaignaudience.ErrLeadsMetadataUnavailable) {
		t.Fatalf("recipients = %v", err)
	}
}

func TestOfficialCreateHandsItsAutomationToTheAudience(t *testing.T) {
	w := newWorld(t)
	w.channel.slots = []campaignaudience.Slot{{Key: "1"}}
	w.channel.judge = fakeJudge{}
	w.channel.metadata = needsClass
	preview := w.upload(t, campaignaudience.ChannelWhatsApp, classList, View{})
	inner := &recordingOfficial{}
	input := &wc.Campaign{WorkspaceID: "ws-1", TemplateID: "tpl", WorkflowID: "wf-1", EnableWorkflow: true, Recipients: &campaignaudience.Request{Spec: campaignaudience.Spec{Source: campaignaudience.SourceList, ListID: preview.ID}, UserID: "user-1"}}
	if _, err := OfficialCreate(inner, w.service).Execute(ctx, input); !errors.Is(err, campaignaudience.ErrListMetadataMissing) {
		t.Fatalf("create = %v", err)
	}
	if got := w.channel.params[len(w.channel.params)-1].Automation; got != input.Automation() || len(inner.got) != 0 {
		t.Fatalf("automation = %+v, inner %d", got, len(inner.got))
	}
}

func TestUnofficialCreateHandsItsAutomationToTheAudience(t *testing.T) {
	w := newWorld(t)
	unofficial := &fakeChannel{name: campaignaudience.ChannelUnofficial, slots: []campaignaudience.Slot{{Key: "1"}}, judge: fakeJudge{}, metadata: needsClass}
	w.service.AddChannel(unofficial)
	preview := w.upload(t, campaignaudience.ChannelUnofficial, "numero;nome;variavel1;Turma\n5511900010142;Maria;Maria;\n", View{})
	inner := &recordingUnofficial{}
	in := &uwc.Campaign{WorkspaceID: "ws-1", InstanceID: "inst-1", AgentID: "agent-1", Message: uwc.MessageSpec{Kind: uwc.KindText, Bodies: []string{"Oi {{1}}"}}, Recipients: &campaignaudience.Request{Spec: campaignaudience.Spec{Source: campaignaudience.SourceList, ListID: preview.ID}, UserID: "user-1"}}
	if _, err := UnofficialCreate(inner, w.service).Execute(ctx, in, uw.DepartmentScope{}); !errors.Is(err, campaignaudience.ErrListMetadataMissing) {
		t.Fatalf("create = %v", err)
	}
	if got := unofficial.params[len(unofficial.params)-1].Automation; got != in.Automation() || len(inner.got) != 0 {
		t.Fatalf("automation = %+v, inner %d", got, len(inner.got))
	}
}
