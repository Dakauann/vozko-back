package emaildesigntools

import (
	"context"
	"encoding/json"
	"errors"
	"strings"
	"testing"

	"vozko/domain/copilot"
	"vozko/domain/emailcampaign"
	"vozko/domain/emaildesign"
	"vozko/domain/workspace"
	"vozko/usecases/copilot/copilottools"
	"vozko/usecases/copilot/copilottools/tooltest"
	emailcampaign_usecase "vozko/usecases/emailcampaign"
)

const campaignID = "5f0c7c1e-1d2a-4b8e-9d11-3a2b1c0d9e8f"

var campaignView = copilot.View{Surface: copilot.SurfaceEmailCampaign, ProjectID: campaignID}

type fakeCampaigns struct {
	campaign *emailcampaign.Campaign
	revised  []emailcampaign.Content
	err      error
	reads    int
}

func (f *fakeCampaigns) Get(_ context.Context, a emailcampaign_usecase.Actor, id string) (*emailcampaign.Campaign, error) {
	f.reads++
	if f.campaign == nil || f.campaign.ID != id || f.campaign.WorkspaceID != a.WorkspaceID {
		return nil, emailcampaign.ErrCampaignNotFound
	}
	c := *f.campaign
	return &c, nil
}

func (f *fakeCampaigns) ReviseContent(_ context.Context, a emailcampaign_usecase.Actor, id string, content emailcampaign.Content) (*emailcampaign.Campaign, error) {
	if f.err != nil {
		return nil, f.err
	}
	if f.campaign == nil || f.campaign.ID != id || f.campaign.WorkspaceID != a.WorkspaceID {
		return nil, emailcampaign.ErrCampaignNotFound
	}
	if !f.campaign.Status.ContentRevisable() {
		return nil, emailcampaign.ErrNotRevisable
	}
	f.revised = append(f.revised, content)
	if err := f.campaign.ReviseContent(content); err != nil {
		return nil, err
	}
	f.campaign.Status = f.campaign.Status.AfterEdit()
	c := *f.campaign
	return &c, nil
}

type campaignRig struct {
	*rig
	campaigns *fakeCampaigns
}

func campaignDocument() emaildesign.Document {
	doc := liveDocument()
	doc.Blocks[2] = emaildesign.Block{ID: "intro", Kind: emaildesign.KindText, Text: &emaildesign.Text{Paragraphs: []emaildesign.Paragraph{{Runs: []emaildesign.Run{{Text: "Olá, {{first_name|cliente}}"}}}}}}
	return doc
}

func newCampaignRig(status emailcampaign.Status) *campaignRig {
	r := newRig()
	r.editor.live = campaignDocument()
	r.editor.envelope = &envelope{Subject: "Oferta para {{first_name|você}}", Preheader: "Só hoje"}
	campaigns := &fakeCampaigns{campaign: &emailcampaign.Campaign{ID: campaignID, WorkspaceID: "ws-1", Name: "Black Friday", Status: status, Subject: "Oferta para {{first_name|você}}", Preheader: "Só hoje", Document: campaignDocument()}}
	r.deps.Campaigns = campaigns
	r.cc = tooltest.OwnerWithScreen(campaignView, r.editor)
	return &campaignRig{rig: r, campaigns: campaigns}
}

func resultData(t *testing.T, res copilot.Result) map[string]interface{} {
	t.Helper()
	if res.Status != copilot.StatusOK {
		t.Fatalf("got %+v", res)
	}
	return decoded(t, res.Data)
}

func TestTheReadResolvesTheDesignOrTheCampaignFromTheView(t *testing.T) {
	design := newRig()
	design.deps.Campaigns = &fakeCampaigns{}
	got := resultData(t, NewEmailReadTool(design.deps).Execute(context.Background(), design.cc, nil))
	if _, ok := got["subject"]; ok || got["campaign_status"] != nil || design.deps.Campaigns.(*fakeCampaigns).reads != 0 {
		t.Fatalf("a design has no envelope and never reads a campaign, got %+v", got)
	}

	r := newCampaignRig(emailcampaign.StatusDraft)
	r.editor.live.Settings.Preheader = "do documento"
	got = resultData(t, NewEmailReadTool(r.deps).Execute(context.Background(), r.cc, nil))
	settings := got["settings"].(map[string]interface{})
	if got["subject"] != "Oferta para {{first_name|você}}" || got["campaign_status"] != "draft" || settings["preheader"] != "Só hoje" {
		t.Fatalf("a campaign shows its own subject, status and the preheader that goes out, got %+v", got)
	}
	if len(r.designs.rendered) != 0 || r.campaigns.reads != 1 {
		t.Fatal("a campaign is resolved through the campaigns, never as a design")
	}
}

func TestACampaignOfAnotherWorkspaceIsNeverResolved(t *testing.T) {
	r := newCampaignRig(emailcampaign.StatusDraft)
	r.campaigns.campaign.WorkspaceID = "ws-2"
	for _, tool := range []copilot.Tool{NewEmailReadTool(r.deps), NewEmailEditTool(r.deps)} {
		res := tool.Execute(context.Background(), r.cc, args(t, `{"operations":[{"op":"settings","fields":{"subject":"Oi"}}]}`))
		if res.Status != copilot.StatusError || res.Message != emailCampaignGone {
			t.Fatalf("%s: got %+v", tool.Definition().Name, res)
		}
	}
	if len(r.editor.commands) != 0 || len(r.campaigns.revised) != 0 {
		t.Fatal("nothing reaches the editor or the campaign")
	}
}

func TestEditsRefuseEveryCampaignPastReady(t *testing.T) {
	for _, status := range []emailcampaign.Status{emailcampaign.StatusScheduled, emailcampaign.StatusRunning, emailcampaign.StatusPaused, emailcampaign.StatusHeld, emailcampaign.StatusCompleted, emailcampaign.StatusStopped} {
		r := newCampaignRig(status)
		edit := args(t, `{"operations":[{"op":"set","id":"title","fields":{"text":"Nova"}}]}`)
		if err := NewEmailEditTool(r.deps).(copilot.Validator).Validate(context.Background(), r.cc, edit); !errors.Is(err, copilottools.ErrInvalidArgs) {
			t.Fatalf("%s: validate = %v", status, err)
		}
		res := NewEmailEditTool(r.deps).Execute(context.Background(), r.cc, edit)
		if res.Status != copilot.StatusError || !strings.Contains(res.Message, "não muda") {
			t.Fatalf("%s: got %+v", status, res)
		}
		image := args(t, `{"prompt":"vitrine","block_id":"hero","alt":"Vitrine"}`)
		if err := NewEmailGenerateImageTool(r.deps).(copilot.Validator).Validate(context.Background(), r.cc, image); !errors.Is(err, copilottools.ErrInvalidArgs) {
			t.Fatalf("%s: a generation is refused before it is billed, got %v", status, err)
		}
		if len(r.editor.commands) != 0 || len(r.campaigns.revised) != 0 || r.images.RequestedBy != "" {
			t.Fatalf("%s: nothing reaches the editor, the campaign or the generator", status)
		}
	}
	if !strings.Contains(lockedMessage(emailcampaign.StatusScheduled), "desagenda") {
		t.Fatal("a scheduled campaign tells how to edit it again")
	}
}

func TestReadingAndEditingNeedTheEmailMarketingPermissions(t *testing.T) {
	deps := newRig().deps
	read, edit := NewEmailReadTool(deps).Meta(), NewEmailEditTool(deps).Meta()
	if read.Resource != workspace.ResourceEmailMarketing || read.Action != workspace.ActionRead {
		t.Fatalf("read = %+v", read)
	}
	if edit.Resource != workspace.ResourceEmailMarketing || edit.Action != workspace.ActionUpdate {
		t.Fatalf("edit = %+v", edit)
	}
	extra := NewEmailGenerateImageTool(deps).(interface {
		AlsoRequires() []workspace.PermissionEntry
	}).AlsoRequires()
	if len(extra) != 1 || extra[0].Resource != workspace.ResourceEmailMarketing || extra[0].Action != workspace.ActionUpdate {
		t.Fatalf("generate also needs = %+v", extra)
	}
}

func TestAReadyCampaignThatEloEditsGoesBackToDraft(t *testing.T) {
	r := newCampaignRig(emailcampaign.StatusReady)
	got := resultData(t, NewEmailEditTool(r.deps).Execute(context.Background(), r.cc, args(t, `{"operations":[{"op":"set","id":"title","fields":{"text":"Só hoje"}}]}`)))
	if got["campaign_status"] != "draft" || got["returned_to_draft"] != true {
		t.Fatalf("got %+v", got)
	}
	if len(r.campaigns.revised) != 1 || r.campaigns.campaign.Status != emailcampaign.StatusDraft {
		t.Fatalf("the edit is kept on the server draft, got %+v", r.campaigns.revised)
	}
	draft := newCampaignRig(emailcampaign.StatusDraft)
	got = resultData(t, NewEmailEditTool(draft.deps).Execute(context.Background(), draft.cc, args(t, `{"operations":[{"op":"set","id":"title","fields":{"text":"Só hoje"}}]}`)))
	if got["campaign_status"] != "draft" || got["returned_to_draft"] != nil {
		t.Fatalf("a draft stays a draft without notice, got %+v", got)
	}
}

func TestTheSubjectTravelsInSettingsOnlyOnACampaign(t *testing.T) {
	r := newCampaignRig(emailcampaign.StatusDraft)
	got := resultData(t, NewEmailEditTool(r.deps).Execute(context.Background(), r.cc, args(t, `{"operations":[{"op":"settings","fields":{"subject":"Última chance, {{first_name|cliente}}"}},{"op":"set","id":"title","fields":{"text":"Só hoje"}}]}`)))
	edits := r.editor.sent(copilot.ScreenEdit)
	payload := edits[0].Args.(editPayload)
	if payload.Envelope == nil || payload.Envelope.Next.Subject != "Última chance, {{first_name|cliente}}" || payload.Envelope.Base.Subject != "Oferta para {{first_name|você}}" || len(payload.Steps) != 1 {
		t.Fatalf("the subject goes as an envelope edit next to one document step, got %+v", payload)
	}
	if got["applied"] != float64(2) || r.campaigns.revised[0].Subject != "Última chance, {{first_name|cliente}}" {
		t.Fatalf("got %+v, revised %+v", got, r.campaigns.revised)
	}

	design := newRig()
	res := NewEmailEditTool(design.deps).Execute(context.Background(), design.cc, args(t, `{"operations":[{"op":"settings","fields":{"subject":"Oi"}}]}`))
	if res.Status != copilot.StatusError || res.Message != emailSubjectNoCampaign || len(design.editor.sent(copilot.ScreenEdit)) != 0 {
		t.Fatalf("a design has no subject, got %+v", res)
	}
}

func TestAPreheaderOnACampaignChangesWhatGoesOut(t *testing.T) {
	r := newCampaignRig(emailcampaign.StatusDraft)
	resultData(t, NewEmailEditTool(r.deps).Execute(context.Background(), r.cc, args(t, `{"operations":[{"op":"settings","fields":{"preheader":"Frete grátis"}}]}`)))
	kept := r.campaigns.revised[0]
	if kept.Preheader != "Frete grátis" || kept.Document.Settings.Preheader != "Frete grátis" || kept.RenderDocument().Settings.Preheader != "Frete grátis" {
		t.Fatalf("got %+v", kept)
	}
}

func TestABrokenSubjectIsRefusedBeforeTheEditor(t *testing.T) {
	r := newCampaignRig(emailcampaign.StatusDraft)
	res := NewEmailEditTool(r.deps).Execute(context.Background(), r.cc, args(t, `{"operations":[{"op":"settings","fields":{"subject":"linha\nquebrada"}}]}`))
	if res.Status != copilot.StatusError || !strings.Contains(res.Message, "subject_invalid_characters") || len(r.editor.sent(copilot.ScreenEdit)) != 0 {
		t.Fatalf("got %+v", res)
	}
	empty := newCampaignRig(emailcampaign.StatusDraft)
	gaps := endsWithGaps(t, NewEmailEditTool(empty.deps).Execute(context.Background(), empty.cc, args(t, `{"operations":[{"op":"settings","fields":{"subject":""}}]}`)))
	if len(gaps) != 1 || gaps[0].Code != "subject_empty" || gaps[0].Field != "subject" {
		t.Fatalf("an empty subject is a gap of the draft, got %+v", gaps)
	}
}

func TestThePersonWinsAndHerVersionIsWhatTheCampaignKeeps(t *testing.T) {
	r := newCampaignRig(emailcampaign.StatusDraft)
	r.editor.typing = func(f *fakeEditor) { f.envelope.Subject = "O assunto que ela digitou" }
	got := resultData(t, NewEmailEditTool(r.deps).Execute(context.Background(), r.cc, args(t, `{"operations":[{"op":"settings","fields":{"subject":"O assunto da Elo"}}]}`)))
	conflicts, _ := got["conflicts"].([]interface{})
	if len(conflicts) != 1 || conflicts[0] != "subject" {
		t.Fatalf("the conflict goes back to Elo, got %+v", got)
	}
	if r.campaigns.revised[0].Subject != "O assunto que ela digitou" {
		t.Fatalf("the campaign keeps what the person typed, got %+v", r.campaigns.revised[0])
	}

	typed := newCampaignRig(emailcampaign.StatusDraft)
	hers := campaignDocument()
	hers.Blocks[0].Heading.Text = "Título dela"
	reply, _ := json.Marshal(map[string]interface{}{"document": hers, "conflicts": []string{"blocks.title.heading.text"}, "skipped": []int{}, "subject": "Oferta para {{first_name|você}}", "preheader": "Só hoje"})
	typed.editor.replies = map[copilot.ScreenCommandName]copilot.ScreenReply{copilot.ScreenEdit: {OK: true, Data: reply}}
	resultData(t, NewEmailEditTool(typed.deps).Execute(context.Background(), typed.cc, args(t, `{"operations":[{"op":"set","id":"title","fields":{"text":"Título da Elo"}}]}`)))
	if typed.campaigns.revised[0].Document.Blocks[0].Heading.Text != "Título dela" {
		t.Fatalf("the server draft never gets Elo's version over the person's, got %+v", typed.campaigns.revised[0].Document.Blocks[0])
	}
}

func TestAnEditTheEditorDidNotTakeLeavesTheCampaignAlone(t *testing.T) {
	r := newCampaignRig(emailcampaign.StatusReady)
	r.editor.typing = func(f *fakeEditor) { f.envelope.Subject = "Dela" }
	reply, _ := json.Marshal(map[string]interface{}{"document": campaignDocument(), "conflicts": []string{"subject"}, "skipped": []int{}, "subject": "Oferta para {{first_name|você}}", "preheader": "Só hoje"})
	r.editor.replies = map[copilot.ScreenCommandName]copilot.ScreenReply{copilot.ScreenEdit: {OK: true, Data: reply}}
	got := resultData(t, NewEmailEditTool(r.deps).Execute(context.Background(), r.cc, args(t, `{"operations":[{"op":"settings","fields":{"subject":"Da Elo"}}]}`)))
	if len(r.campaigns.revised) != 0 || r.campaigns.campaign.Status != emailcampaign.StatusReady || got["returned_to_draft"] != nil {
		t.Fatalf("nothing changed, so a ready campaign stays ready, got %+v", got)
	}
}

func TestVariablesAndTheFooterSurviveACampaignEdit(t *testing.T) {
	r := newCampaignRig(emailcampaign.StatusDraft)
	resultData(t, NewEmailEditTool(r.deps).Execute(context.Background(), r.cc, args(t, `{"operations":[{"op":"add","kind":"button","fields":{"label":"Comprar","href":"https://loja.example.com"}},{"op":"set","id":"title","fields":{"text":"Oi, {{first_name|cliente}}"}}]}`)))
	kept := r.campaigns.revised[0]
	blocks := kept.Document.Blocks
	if blocks[len(blocks)-1].Kind != emaildesign.KindFooter || blocks[len(blocks)-1].Footer.CompanyName != "Loja" {
		t.Fatalf("the footer stays last, got %+v", blocks[len(blocks)-1])
	}
	intro, _ := emaildesign.FindBlock(blocks, "intro")
	title, _ := emaildesign.FindBlock(blocks, "title")
	if intro.Text.Paragraphs[0].Runs[0].Text != "Olá, {{first_name|cliente}}" || title.Heading.Text != "Oi, {{first_name|cliente}}" || kept.Subject != "Oferta para {{first_name|você}}" {
		t.Fatalf("merge tags stay as written, got %+v %+v %q", intro, title, kept.Subject)
	}
	if _, err := emaildesign.Render(kept.RenderDocument(), emaildesign.RenderInput{UnsubscribeURL: "https://app.example.com/u", UnsubscribeLabel: "Sair"}); err != nil {
		t.Fatalf("the kept content still renders with its unsubscribe line: %v", err)
	}

	locked := newCampaignRig(emailcampaign.StatusDraft)
	res := NewEmailEditTool(locked.deps).Execute(context.Background(), locked.cc, args(t, `{"operations":[{"op":"settings","fields":{"subject":"Oi"}},{"op":"remove","id":"footer"}]}`))
	if res.Status != copilot.StatusError || !strings.Contains(res.Message, "operação 2") || !strings.Contains(res.Message, "footer_locked") || len(locked.editor.sent(copilot.ScreenEdit)) != 0 {
		t.Fatalf("the footer never goes, and the error names the request's own operation, got %+v", res)
	}
}

func TestTheCampaignReviewLintsTheSubject(t *testing.T) {
	r := newCampaignRig(emailcampaign.StatusDraft)
	r.editor.envelope.Subject = "OFERTA IMPERDÍVEL HOJE"
	got := resultData(t, NewEmailReadTool(r.deps).Execute(context.Background(), r.cc, nil))
	raw, _ := json.Marshal(got["review"])
	if !strings.Contains(string(raw), "subject_all_caps") {
		t.Fatalf("got %s", raw)
	}
}

func TestTheSnapshotNamesTheOpenEmailAndTheReadTool(t *testing.T) {
	design := newRig()
	snapshot := watch(design).ViewSnapshot(context.Background(), design.cc)
	if !strings.Contains(snapshot, `"Black Friday"`) || !strings.Contains(snapshot, designID) || !strings.Contains(snapshot, "email_read") || !strings.Contains(snapshot, "sem pedir id") {
		t.Fatalf("got %q", snapshot)
	}
	campaign := newCampaignRig(emailcampaign.StatusDraft)
	campaign.editor.envelope.Subject = ""
	snapshot = watch(campaign.rig).ViewSnapshot(context.Background(), campaign.cc)
	if !strings.Contains(snapshot, "campanha") || !strings.Contains(snapshot, campaignID) || !strings.Contains(snapshot, "email_read") || !strings.Contains(snapshot, "subject_empty") {
		t.Fatalf("got %q", snapshot)
	}
	silent := newRig()
	silent.editor.err = copilot.ErrScreenUnavailable
	snapshot = watch(silent).ViewSnapshot(context.Background(), silent.cc)
	if !strings.Contains(snapshot, designID) || !strings.Contains(snapshot, "email_read") || strings.Contains(snapshot, "nada impede") {
		t.Fatalf("a silent editor still names the open email, got %q", snapshot)
	}
}

func TestEveryEmailToolIsOfferedOnBothEmailSurfaces(t *testing.T) {
	for _, view := range []copilot.View{designView, campaignView} {
		for _, tool := range EmailDesignTools(Deps{}) {
			name := tool.Definition().Name
			if offered := view.Offers(tool); offered == (name == "new_email_design") {
				t.Fatalf("%s on %s: offered = %v", name, view.Surface, offered)
			}
		}
	}
}
