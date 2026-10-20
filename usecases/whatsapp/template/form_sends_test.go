package template_usecase

import (
	"context"
	"errors"
	"sync"
	"testing"

	"vozko/domain/whatsapp/template"
	wf "vozko/domain/whatsappform"
	whatsappform_usecase "vozko/usecases/whatsappform"
)

type formSettlement struct {
	token     string
	messageID string
	err       error
}

type fakeFormSends struct {
	mu        sync.Mutex
	prepared  []whatsappform_usecase.TemplateSend
	settled   []formSettlement
	err       error
	onPrepare func()
}

func (f *fakeFormSends) PrepareTemplateSend(_ context.Context, in whatsappform_usecase.TemplateSend) (wf.Send, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	if f.onPrepare != nil {
		f.onPrepare()
	}
	f.prepared = append(f.prepared, in)
	if f.err != nil {
		return wf.Send{}, f.err
	}
	return wf.Send{ID: "send-1", Token: "flowtok_1", Status: wf.SendPrepared}, nil
}

func (f *fakeFormSends) SettleTemplateSend(_ context.Context, token, messageID string, sendErr error) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.settled = append(f.settled, formSettlement{token: token, messageID: messageID, err: sendErr})
	return nil
}

type fakePublications struct {
	pub *wf.Publication
	err error
	got []string
}

func (p *fakePublications) FindByMetaFlow(_ context.Context, metaWABAID, metaFlowID string) (*wf.Publication, error) {
	p.got = append(p.got, metaWABAID+"|"+metaFlowID)
	return p.pub, p.err
}

func sendablePublication() *wf.Publication {
	return &wf.Publication{ID: "pub-1", WorkspaceID: "ws-1", FormID: "form-1", MetaWABAID: "waba-1", MetaFlowID: "F1", Status: wf.PublicationPublished}
}

func formButtons() template.TemplateComponent {
	return template.TemplateComponent{Type: "BUTTONS", Buttons: []template.TemplateButton{
		{Type: template.ButtonTypeFlow, Text: "Responder", FlowID: "F1", FlowAction: template.FlowActionNavigate, NavigateScreen: "S_INICIO"},
	}}
}

func approvedFormTemplate() *template.Template {
	tmpl := approvedUtility()
	tmpl.Components = []template.TemplateComponent{{Type: "BODY", Text: "Responda o cadastro"}, formButtons()}
	return tmpl
}

func TestCheckFormPublished(t *testing.T) {
	dbDown := errors.New("db down")
	other := sendablePublication()
	other.WorkspaceID = "ws-other"
	blocked := sendablePublication()
	blocked.Status = wf.PublicationBlocked
	noFlowID := sendablePublication()
	noFlowID.MetaFlowID = ""

	cases := []struct {
		name      string
		tmpl      *template.Template
		pubs      *fakePublications
		noPubs    bool
		workspace string
		want      error
		wantNot   error
	}{
		{name: "template without a form needs no publication", tmpl: approvedUtility(), noPubs: true, workspace: "ws-1"},
		{name: "sendable publication of the workspace", tmpl: approvedFormTemplate(), pubs: &fakePublications{pub: sendablePublication()}, workspace: "ws-1"},
		{name: "not published on the account", tmpl: approvedFormTemplate(), pubs: &fakePublications{err: wf.ErrPublicationNotFound}, workspace: "ws-1", want: template.ErrFlowNotPublished},
		{name: "published by another workspace", tmpl: approvedFormTemplate(), pubs: &fakePublications{pub: other}, workspace: "ws-1", want: template.ErrFlowNotPublished},
		{name: "blocked by Meta", tmpl: approvedFormTemplate(), pubs: &fakePublications{pub: blocked}, workspace: "ws-1", want: template.ErrFlowNotPublished},
		{name: "publication without a Meta flow", tmpl: approvedFormTemplate(), pubs: &fakePublications{pub: noFlowID}, workspace: "ws-1", want: template.ErrFlowNotPublished},
		{name: "no publication found and no error", tmpl: approvedFormTemplate(), pubs: &fakePublications{}, workspace: "ws-1", want: template.ErrFlowNotPublished},
		{name: "no workspace to own it", tmpl: approvedFormTemplate(), pubs: &fakePublications{pub: sendablePublication()}, want: template.ErrFlowNotPublished},
		{name: "no publication reader", tmpl: approvedFormTemplate(), noPubs: true, workspace: "ws-1", want: template.ErrFlowNotPublished},
		{name: "unreadable publications stay an infrastructure error", tmpl: approvedFormTemplate(), pubs: &fakePublications{err: dbDown}, workspace: "ws-1", want: dbDown, wantNot: template.ErrFlowNotPublished},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			var pubs FlowPublications
			if !tc.noPubs {
				pubs = tc.pubs
			}

			err := CheckFormPublished(context.Background(), pubs, tc.workspace, "waba-1", tc.tmpl)

			if !errors.Is(err, tc.want) || (tc.want == nil && err != nil) {
				t.Fatalf("err = %v, want %v", err, tc.want)
			}
			if tc.wantNot != nil && errors.Is(err, tc.wantNot) {
				t.Fatalf("err = %v must not read as %v", err, tc.wantNot)
			}
			if tc.pubs != nil && len(tc.pubs.got) > 0 && tc.pubs.got[0] != "waba-1|F1" {
				t.Fatalf("looked up %v, want the template's account and flow", tc.pubs.got)
			}
		})
	}
}

func formHarness(t *testing.T, forms *fakeFormSends) *harness {
	t.Helper()
	return newHarness(t, func(d *BilledTemplateSenderDeps) {
		d.Templates = &stubTemplateRepo{tmpl: approvedFormTemplate()}
		if forms != nil {
			d.Forms = forms
		}
	})
}

func formInput() template.BilledSendInput {
	in := validInput()
	in.EntryID = "entry-1"
	in.LeadID = "lead-1"
	in.Origin = wf.OriginWorkflow
	in.OriginRef = "wf-1"
	in.WorkflowRunID = "run-1"
	return in
}

func TestBilledSend_TemplateWithoutAFormNeverPrepares(t *testing.T) {
	forms := &fakeFormSends{}
	h := newHarness(t, func(d *BilledTemplateSenderDeps) { d.Forms = forms })

	if _, err := h.uc.Execute(context.Background(), validInput()); err != nil {
		t.Fatalf("send: %v", err)
	}
	if len(forms.prepared) != 0 || len(forms.settled) != 0 {
		t.Fatalf("prepared %d settled %d, want nothing for a template without a form", len(forms.prepared), len(forms.settled))
	}
	if h.client.sendCount() != 1 || h.billing.debitCount() != 1 {
		t.Fatalf("sends %d debits %d", h.client.sendCount(), h.billing.debitCount())
	}
}

func TestBilledSend_FormTemplatePreparesBeforeTheChargeAndSettlesWithTheMessage(t *testing.T) {
	forms := &fakeFormSends{}
	h := formHarness(t, forms)
	debitsAtPrepare := -1
	forms.onPrepare = func() { debitsAtPrepare = h.billing.debitCount() }

	res, err := h.uc.Execute(context.Background(), formInput())
	if err != nil {
		t.Fatalf("send: %v", err)
	}
	if debitsAtPrepare != 0 {
		t.Fatalf("the form was prepared after %d debits, want before the charge", debitsAtPrepare)
	}
	want := whatsappform_usecase.TemplateSend{
		WorkspaceID: "ws-1", BusinessPhoneID: "bp-1", ContactNumber: "5511999999999", MetaFlowID: "F1", TemplateID: "tpl-1",
		Origin: wf.OriginWorkflow, OriginRef: "wf-1", EntryID: "entry-1", LeadID: "lead-1", WorkflowRunID: "run-1", SentBy: "user-1",
	}
	if len(forms.prepared) != 1 || forms.prepared[0] != want {
		t.Fatalf("prepared %+v, want %+v", forms.prepared, want)
	}
	buttons := h.client.lastIn.Buttons
	if len(buttons) != 1 || buttons[0].FlowToken != "flowtok_1" {
		t.Fatalf("buttons %+v, want the prepared token on the form button", buttons)
	}
	if len(forms.settled) != 1 || forms.settled[0] != (formSettlement{token: "flowtok_1", messageID: res.MessageID}) {
		t.Fatalf("settled %+v, want the token with message %s", forms.settled, res.MessageID)
	}
	if h.billing.debitCount() != 1 {
		t.Fatalf("debits = %d, want 1", h.billing.debitCount())
	}
}

func TestBilledSend_RefusedFormIsNeitherSentNorCharged(t *testing.T) {
	dbDown := errors.New("db down")
	cases := []struct {
		name      string
		forms     *fakeFormSends
		want      error
		wantCoded bool
	}{
		{name: "publication blocked by Meta", forms: &fakeFormSends{err: wf.ErrPublicationNotSendable}, want: wf.ErrPublicationNotSendable, wantCoded: true},
		{name: "not published on the number's account", forms: &fakeFormSends{err: wf.ErrNoPublication}, want: wf.ErrNoPublication, wantCoded: true},
		{name: "number owned by another workspace", forms: &fakeFormSends{err: wf.ErrNotOwner}, want: wf.ErrNotOwner, wantCoded: true},
		{name: "form sends not wired", want: template.ErrFormNotSendable, wantCoded: true},
		{name: "sends unreadable", forms: &fakeFormSends{err: dbDown}, want: dbDown},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			h := formHarness(t, tc.forms)

			_, err := h.uc.Execute(context.Background(), formInput())

			if !errors.Is(err, tc.want) {
				t.Fatalf("err = %v, want %v", err, tc.want)
			}
			if got := errors.Is(err, template.ErrFormNotSendable); got != tc.wantCoded {
				t.Fatalf("coded as form_not_sendable = %v, want %v (%v)", got, tc.wantCoded, err)
			}
			if h.client.sendCount() != 0 || h.billing.debitCount() != 0 || h.billing.refundCount() != 0 {
				t.Fatalf("sends %d debits %d refunds %d, want nothing", h.client.sendCount(), h.billing.debitCount(), h.billing.refundCount())
			}
			if tc.forms != nil && len(tc.forms.settled) != 0 {
				t.Fatalf("settled %+v, a refused preparation has nothing to settle", tc.forms.settled)
			}
		})
	}
}

func TestBilledSend_FormSendIsSettledOnEveryExitAfterItWasPrepared(t *testing.T) {
	cases := []struct {
		name        string
		mutate      func(*harness)
		wantSends   int
		wantMessage string
		wantRefunds int
	}{
		{name: "provider rejected", mutate: func(h *harness) {
			h.client.err = errors.New("meta said no")
			h.client.out.ResponseStatus = 400
			h.client.out.MessageID = ""
		}, wantSends: 1, wantRefunds: 1},
		{name: "transport unknown", mutate: func(h *harness) {
			h.client.err = errors.New("connection reset")
			h.client.out = nil
		}, wantSends: 1},
		{name: "accepted without a message id", mutate: func(h *harness) { h.client.out.MessageID = "" }, wantSends: 1},
		{name: "charge refused", mutate: func(h *harness) { h.billing.debitErr = errors.New("insufficient balance") }},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			forms := &fakeFormSends{}
			h := formHarness(t, forms)
			tc.mutate(h)

			_, _ = h.uc.Execute(context.Background(), formInput())

			if h.client.sendCount() != tc.wantSends || h.billing.refundCount() != tc.wantRefunds {
				t.Fatalf("sends %d refunds %d", h.client.sendCount(), h.billing.refundCount())
			}
			if len(forms.settled) != 1 {
				t.Fatalf("settled %+v, want exactly one settlement", forms.settled)
			}
			got := forms.settled[0]
			if got.token != "flowtok_1" || got.messageID != tc.wantMessage || got.err == nil {
				t.Fatalf("settlement %+v, want the token marked as not delivered", got)
			}
		})
	}
}

func TestBilledSend_ReplayOfAFormSendPreparesNothingNew(t *testing.T) {
	forms := &fakeFormSends{}
	h := formHarness(t, forms)

	for range 2 {
		if _, err := h.uc.Execute(context.Background(), formInput()); err != nil {
			t.Fatalf("send: %v", err)
		}
	}

	if len(forms.prepared) != 1 || len(forms.settled) != 1 || h.client.sendCount() != 1 {
		t.Fatalf("prepared %d settled %d sends %d, want one of each", len(forms.prepared), len(forms.settled), h.client.sendCount())
	}
}

func TestBilledSend_FormTemplateThatCannotBeBuiltIsRefusedBeforeAnyPreparation(t *testing.T) {
	forms := &fakeFormSends{}
	unnamed := approvedFormTemplate()
	unnamed.Name = ""
	h := newHarness(t, func(d *BilledTemplateSenderDeps) {
		d.Templates = &stubTemplateRepo{tmpl: unnamed}
		d.Forms = forms
	})

	if _, err := h.uc.Execute(context.Background(), formInput()); err == nil {
		t.Fatal("a send Meta would refuse must be refused here")
	}
	if len(forms.prepared) != 0 || h.billing.debitCount() != 0 {
		t.Fatalf("prepared %d debits %d", len(forms.prepared), h.billing.debitCount())
	}
	if len(h.attempts.byID) != 0 {
		t.Fatalf("attempts %d, want none for a send refused before billing", len(h.attempts.byID))
	}
}
