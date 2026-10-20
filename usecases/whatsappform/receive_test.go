package whatsappform_usecase

import (
	"errors"
	"fmt"
	"strings"
	"testing"

	"vozko/domain/lead"
	wf "vozko/domain/whatsappform"
	lead_usecase "vozko/usecases/lead"
)

func receiver(t *testing.T, starter string) (*world, FormDetail, wf.Send) {
	t.Helper()
	w := newWorld(t, wf.CapabilityView, wf.CapabilityBuild, wf.CapabilityPublish, wf.CapabilitySend, wf.CapabilityResponses)
	detail := w.create("Cadastro", starter)
	if _, err := w.svc.Publish(ctx, w.viewer, detail.Form.ID); err != nil {
		t.Fatal(err)
	}
	send, err := w.svc.SendInConversation(ctx, w.viewer, SendInput{EntryID: "entry-1", FormID: detail.Form.ID, Copy: formCopy})
	if err != nil {
		t.Fatal(err)
	}
	return w, detail, send
}

func answer(send wf.Send, body string) InboundForm {
	return InboundForm{
		WorkspaceID: ws, BusinessPhoneID: "p1", ContactNumber: "5511987654321", EntryID: "entry-1", EntryType: "whatsapp", LeadID: "lead-1",
		InboundMessageID: "wamid.in-1", ContextMessageID: send.MessageID,
		ResponseJSON: fmt.Sprintf(`{"flow_token": %q, %s}`, send.Token, body), SubmittedAt: baseNow,
	}
}

const leadCaptureAnswers = `"f_nome": "Ana Souza", "f_email": "ana@example.com", "f_interesse": "o_ab", "f_aceite": true`

func TestAMatchedAnswerIsStoredAppliedAndShownMasked(t *testing.T) {
	w, detail, send := receiver(t, "lead_capture")
	w.effects.profileOut = lead_usecase.ProfileResult{Changed: []string{"name", "email"}}
	got, err := w.svc.Receive(ctx, answer(send, leadCaptureAnswers))
	if err != nil {
		t.Fatal(err)
	}
	resp := w.answers.all()[0]
	if resp.Match != wf.MatchByToken || resp.FormID != detail.Form.ID || resp.SendID != send.ID || resp.Origin != wf.OriginInbox || resp.ApplyStatus != wf.ApplyApplied {
		t.Fatalf("response = %+v", resp)
	}
	if len(w.effects.profiles) != 1 {
		t.Fatal("bound answers update the lead profile once")
	}
	update := w.effects.profiles[0]
	if update.LeadID != "lead-1" || update.Source != lead.ProfileFromForm || update.Profile.Name != "Ana Souza" || update.Profile.Email != "ana@example.com" {
		t.Fatalf("profile update = %+v", update)
	}
	if len(w.effects.consents) != 1 || w.effects.consents[0].Evidence.Ref != resp.ID || resp.Report.Consent != ConsentGranted {
		t.Fatalf("consent = %+v report = %+v", w.effects.consents, resp.Report)
	}
	if strings.Join(resp.Report.Changed, ",") != "name,email" {
		t.Fatalf("report = %+v", resp.Report)
	}
	if !strings.HasPrefix(got.Text, "Formulário \"Cadastro\" respondido\nComo podemos te chamar?: Ana Souza") {
		t.Fatalf("text = %q", got.Text)
	}
	card, ok := got.Metadata[wf.MetadataKey].(wf.ResponseCard)
	if !ok || card.ResponseID != resp.ID || len(card.Changed) != 2 {
		t.Fatalf("metadata = %+v", got.Metadata)
	}
	if sends := w.sends.all(); sends[0].Status != wf.SendResponded {
		t.Fatalf("the send is answered, got %+v", sends[0])
	}
	if len(w.effects.triggered) != 1 || w.effects.triggered[0].FormID != detail.Form.ID || w.effects.triggered[0].Data["form.f_nome"] != "Ana Souza" {
		t.Fatalf("trigger = %+v", w.effects.triggered)
	}
}

func TestARedeliveredAnswerIsNotAppliedTwice(t *testing.T) {
	w, _, send := receiver(t, "lead_capture")
	in := answer(send, leadCaptureAnswers)
	if _, err := w.svc.Receive(ctx, in); err != nil {
		t.Fatal(err)
	}
	again, err := w.svc.Receive(ctx, in)
	if err != nil {
		t.Fatal(err)
	}
	if len(w.answers.all()) != 1 || len(w.effects.profiles) != 1 || len(w.effects.consents) != 1 || len(w.effects.triggered) != 1 {
		t.Fatalf("a redelivery changes nothing: %d responses, %d profiles, %d consents, %d triggers",
			len(w.answers.all()), len(w.effects.profiles), len(w.effects.consents), len(w.effects.triggered))
	}
	if again.Response.ID != w.answers.all()[0].ID || again.Text == "" {
		t.Fatalf("the redelivery still returns the card, got %+v", again)
	}
}

func TestAnInterruptedApplyContinuesWithoutRepeatingFinishedSteps(t *testing.T) {
	w, _, send := receiver(t, "lead_capture")
	w.effects.profileErr = errors.New("database down")
	if _, err := w.svc.Receive(ctx, answer(send, leadCaptureAnswers)); err == nil {
		t.Fatal("an infrastructure failure must be retried, not swallowed")
	}
	if resp := w.answers.all()[0]; resp.ApplyStatus != wf.ApplyPending {
		t.Fatalf("the response waits to be applied, got %+v", resp)
	}
	w.effects.profileErr = nil
	if _, err := w.svc.Receive(ctx, answer(send, leadCaptureAnswers)); err != nil {
		t.Fatal(err)
	}
	if resp := w.answers.all()[0]; resp.ApplyStatus != wf.ApplyApplied || len(w.effects.triggered) != 1 {
		t.Fatalf("resp = %+v triggers = %d", resp, len(w.effects.triggered))
	}
}

func TestAnInvalidValueForTheLeadIsAConflictNotARetry(t *testing.T) {
	w, _, send := receiver(t, "lead_capture")
	w.effects.profileErr = lead.ErrLeadEmailInvalid
	if _, err := w.svc.Receive(ctx, answer(send, leadCaptureAnswers)); err != nil {
		t.Fatal(err)
	}
	resp := w.answers.all()[0]
	if resp.ApplyStatus != wf.ApplyPartial || len(resp.Report.Refused) != 1 || resp.Report.Refused[0] != "lead_email_invalid" {
		t.Fatalf("response = %+v", resp)
	}
}

func TestUnmatchedAnswersAreKeptButNeverWriteToTheLead(t *testing.T) {
	cases := []struct {
		name  string
		build func(wf.Send) InboundForm
		want  wf.MatchResult
	}{
		{"no token and no reply context", func(s wf.Send) InboundForm {
			in := answer(s, `"x": 1`)
			in.ResponseJSON, in.ContextMessageID = `{"x": 1}`, ""
			return in
		}, wf.UnmatchedNoToken},
		{"a token Vozko never issued", func(s wf.Send) InboundForm {
			in := answer(s, `"x": 1`)
			in.ResponseJSON, in.ContextMessageID = `{"flow_token": "unused", "x": 1}`, ""
			return in
		}, wf.UnmatchedUnknownToken},
		{"another contact answering", func(s wf.Send) InboundForm {
			in := answer(s, leadCaptureAnswers)
			in.ContactNumber = "5511900000000"
			return in
		}, wf.UnmatchedSender},
		{"another workspace", func(s wf.Send) InboundForm {
			in := answer(s, leadCaptureAnswers)
			in.WorkspaceID = otherWS
			return in
		}, wf.UnmatchedSender},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			w, _, send := receiver(t, "lead_capture")
			got, err := w.svc.Receive(ctx, tc.build(send))
			if err != nil {
				t.Fatal(err)
			}
			resp := w.answers.all()[0]
			if resp.Match != tc.want || resp.FormID != "" || resp.ApplyStatus != wf.ApplySkipped {
				t.Fatalf("response = %+v", resp)
			}
			if len(w.effects.profiles)+len(w.effects.consents)+len(w.effects.triggered)+len(w.effects.resumed) != 0 {
				t.Fatal("an unmatched answer has no effect on the lead or automations")
			}
			if w.sends.all()[0].Status != wf.SendSent {
				t.Fatal("the real send is untouched")
			}
			if got.Text == "" {
				t.Fatal("the conversation still shows that something arrived")
			}
		})
	}
}

func TestTheReplyContextMatchesWhenTheTokenIsMissing(t *testing.T) {
	w, _, send := receiver(t, "lead_capture")
	in := answer(send, leadCaptureAnswers)
	in.ResponseJSON = `{` + leadCaptureAnswers + `}`
	if _, err := w.svc.Receive(ctx, in); err != nil {
		t.Fatal(err)
	}
	if resp := w.answers.all()[0]; resp.Match != wf.MatchByContext || resp.ApplyStatus != wf.ApplyApplied {
		t.Fatalf("response = %+v", resp)
	}
}

func TestAWorkflowRunWaitingOnTheSendIsResumedWithTheAnswers(t *testing.T) {
	w, detail, _ := receiver(t, "lead_capture")
	send, err := w.svc.SendFromWorkflow(ctx, WorkflowSend{WorkspaceID: ws, EntryID: "entry-1", FormID: detail.Form.ID, Copy: formCopy, RunID: "run-7"})
	if err != nil {
		t.Fatal(err)
	}
	in := answer(send, leadCaptureAnswers)
	got, err := w.svc.Receive(ctx, in)
	if err != nil {
		t.Fatal(err)
	}
	if len(w.effects.resumed) != 1 || w.effects.resumed[0].runID != "run-7" || got.ResumedRunID != "run-7" {
		t.Fatalf("resumed = %+v", w.effects.resumed)
	}
	data := w.effects.resumed[0].data
	if data["form_id"] != detail.Form.ID || data["form_response_id"] != got.Response.ID || data["form.f_email"] != "ana@example.com" || data["form.f_aceite"] != "Sim" {
		t.Fatalf("run data = %+v", data)
	}
	if _, err := w.svc.Receive(ctx, in); err != nil || len(w.effects.resumed) != 1 {
		t.Fatalf("a redelivery never resumes twice: %v %d", err, len(w.effects.resumed))
	}
}

func TestFilesAreStoredAndAFailedFileIsRecordedNotRetried(t *testing.T) {
	w, _, send := receiver(t, "documents")
	body := `"f_nome": "Ana", "f_documento": [{"file_name": "rg.pdf", "mime_type": "application/pdf", "sha256": "abc", "id": "meta-media-1"}]`
	if _, err := w.svc.Receive(ctx, answer(send, body)); err != nil {
		t.Fatal(err)
	}
	resp := w.answers.all()[0]
	if len(w.effects.stored) != 1 || w.effects.stored[0].File.ID != "meta-media-1" || w.effects.stored[0].FieldKey != "f_documento" {
		t.Fatalf("stored = %+v", w.effects.stored)
	}
	if len(resp.Report.Files) != 1 || resp.Report.Files[0].MediaID != "media-meta-media-1" {
		t.Fatalf("report = %+v", resp.Report)
	}

	w2, _, send2 := receiver(t, "documents")
	w2.effects.mediaErr = errors.New("hash mismatch")
	if _, err := w2.svc.Receive(ctx, answer(send2, body)); err != nil {
		t.Fatal(err)
	}
	resp2 := w2.answers.all()[0]
	if resp2.ApplyStatus != wf.ApplyPartial || resp2.Report.Files[0].Problem != FileStoreFailed {
		t.Fatalf("response = %+v", resp2)
	}
}

func TestAnAnswerThatIsNotJSONIsRefused(t *testing.T) {
	w, _, send := receiver(t, "lead_capture")
	in := answer(send, leadCaptureAnswers)
	in.ResponseJSON = "not json"
	if _, err := w.svc.Receive(ctx, in); !errors.Is(err, wf.ErrResponseInvalid) {
		t.Fatalf("err = %v", err)
	}
}

func TestReadingResponsesMasksSensitiveAnswersForMostReaders(t *testing.T) {
	w, detail, send := receiver(t, "documents")
	body := `"f_nome": "Ana", "f_documento": [{"file_name": "rg.pdf", "mime_type": "application/pdf", "sha256": "abc", "id": "meta-media-1"}]`
	got, err := w.svc.Receive(ctx, answer(send, body))
	if err != nil {
		t.Fatal(err)
	}
	page, err := w.svc.ListResponses(ctx, w.viewer, wf.ResponseQuery{FormID: detail.Form.ID})
	if err != nil {
		t.Fatal(err)
	}
	if len(page.Items) != 1 || page.Items[0].LeadName != "Ana Souza" || !page.Items[0].Response.Answers.Values["f_documento"].Masked {
		t.Fatalf("page = %+v", page.Items)
	}
	detailView, err := w.svc.GetResponse(ctx, w.viewer, got.Response.ID)
	if err != nil || detailView.Revealed || detailView.Version.ID == "" || detailView.Form.Name != "Cadastro" {
		t.Fatalf("detail = %+v %v", detailView, err)
	}
	sensitive := newWorld(t, wf.CapabilityView, wf.CapabilityResponses, wf.CapabilityResponsesSensitive)
	sensitive.forms, sensitive.pubs, sensitive.answers, sensitive.sends = w.forms, w.pubs, w.answers, w.sends
	sensitive.build()
	revealed, err := sensitive.svc.GetResponse(ctx, sensitive.viewer, got.Response.ID)
	if err != nil || !revealed.Revealed || revealed.Response.Answers.Values["f_documento"].Files[0].Name != "rg.pdf" {
		t.Fatalf("revealed = %+v %v", revealed, err)
	}
}

func TestResponsesNeedTheResponsesCapabilityAndAResolvedScope(t *testing.T) {
	w, detail, _ := receiver(t, "lead_capture")
	viewerOnly := newWorld(t, wf.CapabilityView)
	viewerOnly.forms = w.forms
	viewerOnly.build()
	if _, err := viewerOnly.svc.ListResponses(ctx, viewerOnly.viewer, wf.ResponseQuery{FormID: detail.Form.ID}); !errors.Is(err, wf.ErrForbidden) {
		t.Fatalf("err = %v", err)
	}
	w.targets.scopeOK = false
	if _, err := w.svc.ListResponses(ctx, w.viewer, wf.ResponseQuery{FormID: detail.Form.ID}); !errors.Is(err, wf.ErrForbidden) {
		t.Fatalf("a reader whose department scope cannot be resolved sees nothing, got %v", err)
	}
}

func TestDeletingAResponseNeedsItsCapability(t *testing.T) {
	w, _, send := receiver(t, "lead_capture")
	got, err := w.svc.Receive(ctx, answer(send, leadCaptureAnswers))
	if err != nil {
		t.Fatal(err)
	}
	if err := w.svc.DeleteResponse(ctx, w.viewer, got.Response.ID); !errors.Is(err, wf.ErrForbidden) {
		t.Fatalf("err = %v", err)
	}
	deleter := newWorld(t, wf.CapabilityView, wf.CapabilityResponses, wf.CapabilityResponsesDelete)
	deleter.answers = w.answers
	deleter.build()
	if err := deleter.svc.DeleteResponse(ctx, deleter.viewer, got.Response.ID); err != nil {
		t.Fatal(err)
	}
	if len(w.answers.all()) != 0 {
		t.Fatal("the response is gone")
	}
}
