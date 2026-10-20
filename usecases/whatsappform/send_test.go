package whatsappform_usecase

import (
	"errors"
	"testing"

	"vozko/domain/conversation"
	businessphone "vozko/domain/whatsapp/business_phone"
	wf "vozko/domain/whatsappform"
)

var formCopy = wf.SendCopy{Body: "Preencha seus dados", CTA: "Abrir formulário"}

func sender(t *testing.T) (*world, FormDetail) {
	t.Helper()
	w := newWorld(t, wf.CapabilityView, wf.CapabilityBuild, wf.CapabilityPublish, wf.CapabilitySend)
	detail := w.create("Cadastro", "")
	if _, err := w.svc.Publish(ctx, w.viewer, detail.Form.ID); err != nil {
		t.Fatal(err)
	}
	return w, detail
}

func TestSendInAConversationDeliversTheFormWithItsOwnToken(t *testing.T) {
	w, detail := sender(t)
	send, err := w.svc.SendInConversation(ctx, w.viewer, SendInput{EntryID: "entry-1", FormID: detail.Form.ID, Copy: formCopy})
	if err != nil {
		t.Fatal(err)
	}
	if len(w.sender.sent) != 1 {
		t.Fatalf("sent = %+v", w.sender.sent)
	}
	got := w.sender.sent[0]
	pub := w.pubs.only(t, wabaA)
	if got.phoneID != "p1" || got.input.FlowID != pub.MetaFlowID || got.input.To != "5511987654321" || got.input.CTA != formCopy.CTA || !wf.IsVozkoToken(got.input.FlowToken) {
		t.Fatalf("flow message = %+v", got)
	}
	stored := w.sends.all()[0]
	if stored.Status != wf.SendSent || stored.Token != got.input.FlowToken || stored.MessageID != send.MessageID || stored.Origin != wf.OriginInbox ||
		stored.VersionID != pub.VersionID || stored.LeadID != "lead-1" || stored.SentBy != member || stored.EntryID != "entry-1" {
		t.Fatalf("send = %+v", stored)
	}
	if len(w.effects.outbound) != 1 {
		t.Fatal("the form message is recorded in the conversation")
	}
	card, ok := w.effects.outbound[0].Metadata[wf.MetadataKey].(wf.MessageCard)
	if !ok || card.SendID != stored.ID || card.FormName != "Cadastro" || card.Kind != wf.CardMessage || w.effects.outbound[0].SentBy != member {
		t.Fatalf("outbound = %+v", w.effects.outbound[0])
	}
}

func TestSendRefusals(t *testing.T) {
	cases := []struct {
		name   string
		setup  func(*world)
		entry  string
		copy   wf.SendCopy
		want   error
		noSend bool
	}{
		{name: "invalid copy", entry: "entry-1", copy: wf.SendCopy{Body: "x"}, want: wf.ErrCopyInvalid},
		{name: "conversation out of reach", entry: "entry-1", copy: formCopy, setup: func(w *world) { w.targets.denied["entry-1"] = true }, want: wf.ErrForbidden},
		{name: "closed window", entry: "entry-closed", copy: formCopy, want: wf.ErrOutsideWindow},
		{name: "number of another workspace", entry: "entry-foreign", copy: formCopy, want: wf.ErrNotOwner},
		{name: "not published on that account", entry: "entry-1", copy: formCopy, setup: func(w *world) {
			w.pubs.mu.Lock()
			for id, p := range w.pubs.rows {
				if p.MetaWABAID == wabaA {
					delete(w.pubs.rows, id)
				}
			}
			w.pubs.mu.Unlock()
		}, want: wf.ErrNoPublication},
		{name: "blocked by Meta", entry: "entry-1", copy: formCopy, setup: func(w *world) {
			w.pubs.mu.Lock()
			for id, p := range w.pubs.rows {
				p.Status = wf.PublicationBlocked
				w.pubs.rows[id] = p
			}
			w.pubs.mu.Unlock()
		}, want: wf.ErrPublicationNotSendable},
		{name: "a 360dialog number", entry: "entry-1", copy: formCopy, setup: func(w *world) {
			w.phones.phones[0].Provider = businessphone.ProviderDialog360
		}, want: wf.ErrProviderUnsupported},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			w, detail := sender(t)
			if tc.setup != nil {
				tc.setup(w)
			}
			_, err := w.svc.SendInConversation(ctx, w.viewer, SendInput{EntryID: tc.entry, FormID: detail.Form.ID, Copy: tc.copy})
			if !errors.Is(err, tc.want) {
				t.Fatalf("err = %v, want %v", err, tc.want)
			}
			if len(w.sender.sent) != 0 || len(w.sends.all()) != 0 {
				t.Fatal("a refused send never reaches Meta and leaves no send behind")
			}
		})
	}
}

func TestSendWithoutTheCapability(t *testing.T) {
	w, detail := sender(t)
	reader := newWorld(t, wf.CapabilityView)
	reader.forms, reader.pubs = w.forms, w.pubs
	reader.build()
	if _, err := reader.svc.SendInConversation(ctx, reader.viewer, SendInput{EntryID: "entry-1", FormID: detail.Form.ID, Copy: formCopy}); !errors.Is(err, wf.ErrForbidden) {
		t.Fatalf("err = %v", err)
	}
}

func TestAProviderFailureIsAFailedSend(t *testing.T) {
	w, detail := sender(t)
	w.sender.err = conversation.ErrWindowClosed
	if _, err := w.svc.SendInConversation(ctx, w.viewer, SendInput{EntryID: "entry-1", FormID: detail.Form.ID, Copy: formCopy}); !errors.Is(err, conversation.ErrWindowClosed) {
		t.Fatalf("err = %v", err)
	}
	stored := w.sends.all()[0]
	if stored.Status != wf.SendFailed || stored.FailureReason != FailureOutsideWindow || len(w.effects.outbound) != 0 {
		t.Fatalf("send = %+v", stored)
	}
}

func TestWorkflowSendsAreAutomationMessagesTiedToTheRun(t *testing.T) {
	w, detail := sender(t)
	send, err := w.svc.SendFromWorkflow(ctx, WorkflowSend{WorkspaceID: ws, EntryID: "entry-1", FormID: detail.Form.ID, Copy: formCopy, RunID: "run-7"})
	if err != nil {
		t.Fatal(err)
	}
	if send.Origin != wf.OriginWorkflow || send.WorkflowRunID != "run-7" || send.SentBy != "" {
		t.Fatalf("send = %+v", send)
	}
	if out := w.effects.outbound[0]; out.SentBy != "" || !out.Automated {
		t.Fatalf("outbound = %+v", out)
	}
	if _, err := w.svc.SendFromWorkflow(ctx, WorkflowSend{WorkspaceID: otherWS, EntryID: "entry-1", FormID: detail.Form.ID, Copy: formCopy, RunID: "run-8"}); !errors.Is(err, wf.ErrFormNotFound) {
		t.Fatalf("a workflow only sends forms of its own workspace, got %v", err)
	}
}

func TestTemplateSendsMintATokenAndSettleAfterTheProviderAnswers(t *testing.T) {
	w, detail := sender(t)
	pub := w.pubs.only(t, wabaA)
	in := TemplateSend{WorkspaceID: ws, BusinessPhoneID: "p1", ContactNumber: "5511900001111", MetaFlowID: pub.MetaFlowID, TemplateID: "tpl-1", Origin: wf.OriginCampaign, OriginRef: "entry-9", EntryID: "entry-9"}
	prepared, err := w.svc.PrepareTemplateSend(ctx, in)
	if err != nil {
		t.Fatal(err)
	}
	if !wf.IsVozkoToken(prepared.Token) || prepared.Status != wf.SendPrepared || prepared.FormID != detail.Form.ID || prepared.VersionID != pub.VersionID {
		t.Fatalf("prepared = %+v", prepared)
	}
	if err := w.svc.SettleTemplateSend(ctx, prepared.Token, "wamid.tpl", nil); err != nil {
		t.Fatal(err)
	}
	if got := w.sends.all()[0]; got.Status != wf.SendSent || got.MessageID != "wamid.tpl" {
		t.Fatalf("settled = %+v", got)
	}
	failed, _ := w.svc.PrepareTemplateSend(ctx, in)
	if err := w.svc.SettleTemplateSend(ctx, failed.Token, "", errors.New("rejected")); err != nil {
		t.Fatal(err)
	}
	for _, s := range w.sends.all() {
		if s.Token == failed.Token && (s.Status != wf.SendFailed || s.FailureReason != FailureProvider) {
			t.Fatalf("failed send = %+v", s)
		}
	}
}

func TestTemplateSendsRefuseAFlowThatIsNotThisWorkspacesForm(t *testing.T) {
	w, _ := sender(t)
	cases := []TemplateSend{
		{WorkspaceID: ws, BusinessPhoneID: "p1", ContactNumber: "5511900001111", MetaFlowID: "flow-from-manager"},
		{WorkspaceID: ws, BusinessPhoneID: "p4", ContactNumber: "5511900001111", MetaFlowID: w.pubs.only(t, wabaA).MetaFlowID},
	}
	for _, in := range cases {
		if _, err := w.svc.PrepareTemplateSend(ctx, in); err == nil {
			t.Errorf("%+v must be refused", in)
		}
	}
}

func TestSendableFormsForANumber(t *testing.T) {
	w, detail := sender(t)
	w.create("Rascunho", "")
	forms, err := w.svc.Sendable(ctx, w.viewer, SendableQuery{PhoneID: "p3"})
	if err != nil {
		t.Fatal(err)
	}
	pub := w.pubs.only(t, wabaB)
	if len(forms) != 1 || forms[0].FormID != detail.Form.ID || forms[0].MetaFlowID != pub.MetaFlowID || forms[0].FirstScreen != "SCREEN_A" {
		t.Fatalf("sendable = %+v", forms)
	}
	byWABA, err := w.svc.Sendable(ctx, w.viewer, SendableQuery{MetaWABAID: wabaA})
	if err != nil || len(byWABA) != 1 {
		t.Fatalf("sendable by account = %+v %v", byWABA, err)
	}
	if _, err := w.svc.Sendable(ctx, w.viewer, SendableQuery{PhoneID: "p4"}); !errors.Is(err, wf.ErrNotOwner) {
		t.Fatalf("err = %v", err)
	}
	if _, err := w.svc.Sendable(ctx, w.viewer, SendableQuery{MetaWABAID: "waba-other"}); !errors.Is(err, wf.ErrNotOwner) {
		t.Fatalf("err = %v", err)
	}
}

func TestASendFromEloIsRecordedAsElo(t *testing.T) {
	w, detail := sender(t)
	send, err := w.svc.SendInConversation(ctx, w.viewer, SendInput{EntryID: "entry-1", FormID: detail.Form.ID, Copy: formCopy, Origin: wf.OriginElo})
	if err != nil || send.Origin != wf.OriginElo {
		t.Fatalf("send = %+v %v", send, err)
	}
	other, err := w.svc.SendInConversation(ctx, w.viewer, SendInput{EntryID: "entry-1", FormID: detail.Form.ID, Copy: formCopy, Origin: wf.OriginCampaign})
	if err != nil || other.Origin != wf.OriginInbox {
		t.Fatalf("a person in a conversation sends from the inbox or through Elo, got %+v %v", other, err)
	}
}
