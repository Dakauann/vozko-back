package emaildesigntools

import (
	"context"
	"encoding/json"
	"errors"
	"strings"
	"testing"

	"vozko/domain/copilot"
	"vozko/domain/emaildesign"
	"vozko/usecases/copilot/copilottools/tooltest"
)

func args(t *testing.T, raw string) map[string]interface{} {
	t.Helper()
	var out map[string]interface{}
	if err := json.Unmarshal([]byte(raw), &out); err != nil {
		t.Fatal(err)
	}
	return out
}

func TestEmailToolsAreOfferedOnlyInTheEmailEditor(t *testing.T) {
	for _, tool := range EmailDesignTools(Deps{}) {
		name := tool.Definition().Name
		if name == "new_email_design" {
			if designView.Offers(tool) || !(copilot.View{}).Offers(tool) {
				t.Errorf("%s belongs to the panel", name)
			}
			continue
		}
		if !designView.Offers(tool) || (copilot.View{}).Offers(tool) || (copilot.View{Surface: copilot.SurfaceStudio, ProjectID: designID, ProjectKind: copilot.StudioImage}).Offers(tool) {
			t.Errorf("%s belongs to the email editor only", name)
		}
	}
}

func TestEmailReadShowsTheLiveDesignWithItsReview(t *testing.T) {
	r := newRig()
	r.editor.selected = "intro"
	res := NewEmailReadTool(r.deps).Execute(context.Background(), r.cc, nil)
	if res.Status != copilot.StatusOK {
		t.Fatalf("got %+v", res)
	}
	data := decoded(t, res.Data)
	blocks := data["blocks"].([]interface{})
	intro := blocks[2].(map[string]interface{})
	if data["selected"] != "intro" || data["name"] != "Black Friday" || intro["text"] != "Olá" || intro["kind"] != "text" {
		t.Fatalf("got %+v", data)
	}
	review := data["review"].(map[string]interface{})
	if _, ok := review["checks"]; !ok {
		t.Fatalf("the read carries the backend review, got %+v", review)
	}
}

func TestEmailReadFailsClosedOutsideTheEditorOrWithAnUnreadableReply(t *testing.T) {
	r := newRig()
	if res := NewEmailReadTool(r.deps).Execute(context.Background(), tooltest.OwnerOn(copilot.View{}), nil); res.Status != copilot.StatusError {
		t.Fatal("outside the email editor nothing is read")
	}
	other := tooltest.OwnerWithScreen(copilot.View{Surface: copilot.SurfaceEmailDesign, ProjectID: "5f0c7c1e-1d2a-4b8e-9d11-3a2b1c0d9e8f"}, r.editor)
	if res := NewEmailReadTool(r.deps).Execute(context.Background(), other, nil); res.Status != copilot.StatusError || res.Message != emailDesignGone {
		t.Fatalf("a design of another workspace is never read, got %+v", res)
	}
	r.editor.replies = map[copilot.ScreenCommandName]copilot.ScreenReply{copilot.ScreenRead: {OK: true, Data: []byte(`{"document":{"version":1,"blocks":[],"extra":1}}`)}}
	if res := NewEmailReadTool(r.deps).Execute(context.Background(), r.cc, nil); res.Status != copilot.StatusError || res.Message != emailUnreadable {
		t.Fatalf("a document outside the contract is refused, got %+v", res)
	}
}

func TestEmailEditSendsTheBatchAsStepsOverTheLiveDesign(t *testing.T) {
	r := newRig()
	res := NewEmailEditTool(r.deps).Execute(context.Background(), r.cc, args(t, `{"operations":[
		{"op":"add","kind":"button","ref":"cta","fields":{"label":"Ver ofertas","href":"https://loja.example.com/bf"},"at":{"after":"intro"}},
		{"op":"set","id":"intro","fields":{"text":"Olá, **{{first_name|cliente}}**"}},
		{"op":"settings","fields":{"preheader":"Até 50% hoje"}}
	]}`))
	if res.Status != copilot.StatusOK {
		t.Fatalf("got %+v", res)
	}
	edits := r.editor.sent(copilot.ScreenEdit)
	if len(edits) != 1 {
		t.Fatalf("one edit command per batch, got %d", len(edits))
	}
	payload := edits[0].Args.(editPayload)
	if len(payload.Steps) != 3 || payload.Steps[0].Focus != "button-1" || payload.Base.Blocks[0].ID != "title" {
		t.Fatalf("got %+v", payload)
	}
	data := decoded(t, res.Data)
	created := data["created"].(map[string]interface{})
	if created["cta"] != "button-1" || data["applied"] != float64(3) {
		t.Fatalf("got %+v", data)
	}
	if r.editor.live.Settings.Preheader != "Até 50% hoje" {
		t.Fatal("the editor received the batch")
	}
}

func TestEmailEditResolvesLibraryMediaIntoAPublicSource(t *testing.T) {
	r := newRig()
	res := NewEmailEditTool(r.deps).Execute(context.Background(), r.cc, args(t, `{"operations":[{"op":"set","id":"hero","fields":{"media_id":"`+libraryImage+`","alt":"Tênis em promoção"}}]}`))
	if res.Status != copilot.StatusOK {
		t.Fatalf("got %+v", res)
	}
	if got := r.editor.live.Blocks[1].Image.Src; got != cdnBase+"images/ws-1/library.jpg" {
		t.Fatalf("got %q", got)
	}
	for name, raw := range map[string]string{
		"unknown media":    `{"operations":[{"op":"set","id":"hero","fields":{"media_id":"5f0c7c1e-1d2a-4b8e-9d11-3a2b1c0d9e8f"}}]}`,
		"media and source": `{"operations":[{"op":"set","id":"hero","fields":{"media_id":"` + libraryImage + `","src":"https://x.com/a.jpg"}}]}`,
		"invented id":      `{"operations":[{"op":"set","id":"hero","fields":{"media_id":"m1"}}]}`,
	} {
		before := len(r.editor.sent(copilot.ScreenEdit))
		if res := NewEmailEditTool(r.deps).Execute(context.Background(), r.cc, args(t, raw)); res.Status != copilot.StatusError || len(r.editor.sent(copilot.ScreenEdit)) != before {
			t.Errorf("%s: must be refused before reaching the editor, got %+v", name, res)
		}
	}
}

func TestEmailEditRefusesABatchThatBreaksTheContract(t *testing.T) {
	r := newRig()
	res := NewEmailEditTool(r.deps).Execute(context.Background(), r.cc, args(t, `{"operations":[{"op":"set","id":"hero","fields":{"src":"http://inseguro.example.com/a.jpg"}}]}`))
	if res.Status != copilot.StatusError || !strings.Contains(res.Message, "image_src_not_https") || len(r.editor.sent(copilot.ScreenEdit)) != 0 {
		t.Fatalf("an invalid result never reaches the editor, got %+v", res)
	}
	res = NewEmailEditTool(r.deps).Execute(context.Background(), r.cc, args(t, `{"operations":[{"op":"remove","id":"footer"}]}`))
	if res.Status != copilot.StatusError || !strings.Contains(res.Message, "footer_locked") {
		t.Fatalf("the failing operation is named by its code, got %+v", res)
	}
}

func TestEmailEditToleratesProblemsThePersonAlreadyHad(t *testing.T) {
	r := newRig()
	r.editor.live.Blocks[0].Heading.Text = ""
	res := NewEmailEditTool(r.deps).Execute(context.Background(), r.cc, args(t, `{"operations":[{"op":"set","id":"intro","fields":{"text":"Novo"}}]}`))
	if res.Status != copilot.StatusOK {
		t.Fatalf("an old problem does not block a new edit, got %+v", res)
	}
	if gaps := decoded(t, res.Data)["open_gaps"]; !strings.Contains(mustJSON(t, gaps), "text_empty") {
		t.Fatalf("the open gaps still report it, got %+v", gaps)
	}
}

func mustJSON(t *testing.T, v interface{}) string {
	t.Helper()
	raw, err := json.Marshal(v)
	if err != nil {
		t.Fatal(err)
	}
	return string(raw)
}

func TestEmailEditReportsAnEditorThatNeverAnswered(t *testing.T) {
	r := newRig()
	r.editor.replies = map[copilot.ScreenCommandName]copilot.ScreenReply{}
	r.editor.err = nil
	tool := NewEmailEditTool(r.deps)
	silent := &silentEditEditor{fakeEditor: r.editor}
	cc := tooltest.OwnerWithScreen(designView, silent)
	res := tool.Execute(context.Background(), cc, args(t, `{"operations":[{"op":"set","id":"intro","fields":{"text":"Novo"}}]}`))
	if res.Status != copilot.StatusError || res.AwaitsScreen == nil || *res.AwaitsScreen != designView {
		t.Fatalf("a silent editor is awaited on the email screen, got %+v", res)
	}
}

type silentEditEditor struct{ *fakeEditor }

func (s *silentEditEditor) Run(ctx context.Context, cmd copilot.ScreenCommand) (copilot.ScreenReply, error) {
	if cmd.Name == copilot.ScreenEdit {
		return copilot.ScreenReply{}, copilot.ErrScreenUnavailable
	}
	return s.fakeEditor.Run(ctx, cmd)
}

func TestEmailEditNeedsApprovalOnlyWhenAsking(t *testing.T) {
	tool := NewEmailEditTool(Deps{}).(interface{ NeedsApproval(copilot.Mode) bool })
	if !tool.NeedsApproval(copilot.ModeAsk) || tool.NeedsApproval(copilot.ModeEdit) || tool.NeedsApproval(copilot.ModeFull) {
		t.Fatal("edits ask only in ask mode")
	}
	if NewEmailEditTool(Deps{}).Meta() != editMeta() {
		t.Fatal("editing needs the email update permission")
	}
}

func TestEmailEditValidatesBeforeTheApprovalCard(t *testing.T) {
	r := newRig()
	validator := NewEmailEditTool(r.deps).(copilot.Validator)
	if err := validator.Validate(context.Background(), r.cc, args(t, `{"operations":[]}`)); err == nil {
		t.Fatal("an empty batch is refused")
	}
	if err := validator.Validate(context.Background(), r.cc, args(t, `{"operations":[{"op":"set","id":"intro","fields":{"text":"x"}}]}`)); err != nil {
		t.Fatalf("a well formed batch passes, got %v", err)
	}
	var invalid *emaildesign.OperationError
	if err := validator.Validate(context.Background(), r.cc, args(t, `{"operations":[{"op":"paint"}]}`)); err == nil || errors.As(err, &invalid) {
		t.Fatalf("an unknown op is refused as an argument error, got %v", err)
	}
}

func TestEmailEditAppliesADraftAndNamesWhatIsStillMissing(t *testing.T) {
	r := newRig()
	res := NewEmailEditTool(r.deps).Execute(context.Background(), r.cc, args(t, `{"operations":[{"op":"add","kind":"button","fields":{"label":"Agendar consulta"},"at":{"after":"intro"}}]}`))
	if res.Status != copilot.StatusOK || len(r.editor.sent(copilot.ScreenEdit)) != 1 {
		t.Fatalf("a draft with a gap reaches the editor, got %+v", res)
	}
	data := decoded(t, res.Data)
	if !strings.Contains(mustJSON(t, data["open_gaps"]), "href_missing") {
		t.Fatalf("the gap comes back as a new issue to tell the person, got %+v", data)
	}
}
