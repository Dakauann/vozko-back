package formtools

import (
	"context"
	"strings"
	"time"

	"vozko/domain/conversation"
	"vozko/domain/report"
	"vozko/domain/shared"
	wf "vozko/domain/whatsappform"
	report_usecase "vozko/usecases/report"
	whatsappform_usecase "vozko/usecases/whatsappform"
)

const (
	knownForm  = "6c1d2e3f-4a5b-4c6d-8e7f-9a0b1c2d3e4f"
	otherForm  = "1a2b3c4d-5e6f-4a7b-8c9d-0e1f2a3b4c5d"
	knownEntry = "9e8d7c6b-5a49-4382-a1b0-c9d8e7f6a5b4"
	knownPhone = "phone-1"
	knownWABA  = "waba-1"
)

func choiceDocument() wf.Document {
	return wf.Document{
		Schema: wf.DocumentSchema, Categories: []wf.Category{wf.CategoryLeadGeneration},
		Screens: []wf.Screen{
			{Key: "s_dados", Title: "Seus dados", SubmitLabel: "Continuar", Route: wf.Route{Next: "s_interesse"}, Blocks: []wf.Block{
				{Key: "b_nome", Field: &wf.Field{Key: "f_nome", Kind: wf.FieldShortText, Label: "Nome", Required: true, Binding: &wf.Binding{Target: wf.BindName}}},
				{Key: "b_cpf", Field: &wf.Field{Key: "f_cpf", Kind: wf.FieldShortText, Label: "CPF", Sensitive: true}},
			}},
			{Key: "s_interesse", Title: "Interesse", SubmitLabel: "Enviar", Blocks: []wf.Block{
				{Key: "b_titulo", Display: &wf.Display{Kind: wf.DisplayHeading, Text: "Quase lá"}},
				{Key: "b_interesse", Field: &wf.Field{Key: "f_interesse", Kind: wf.FieldRadio, Label: "Interesse", Question: "O que você procura?", Required: true,
					Options: []wf.Option{{ID: "o_aa", Title: "Produtos"}, {ID: "o_ab", Title: "Vendas"}}}},
				{Key: "b_aceite", Field: &wf.Field{Key: "f_aceite", Kind: wf.FieldOptIn, Label: "Aceito receber mensagens.", Binding: &wf.Binding{Target: wf.BindWhatsAppConsent}}},
			}},
		},
	}
}

func knownDetail() whatsappform_usecase.FormDetail {
	doc := choiceDocument()
	return whatsappform_usecase.FormDetail{
		Form:      wf.Form{ID: knownForm, WorkspaceID: "ws-1", Name: "Captação", DraftVersionID: "v-2", PublishedVersionID: "v-1"},
		Draft:     wf.Version{ID: "v-2", Number: 2, Document: doc},
		Published: &wf.Version{ID: "v-1", Number: 1, Document: doc},
		Publications: []wf.Publication{
			{MetaWABAID: knownWABA, Status: wf.PublicationPublished},
			{MetaWABAID: "waba-2", Status: wf.PublicationFailed},
		},
	}
}

type fakeForms struct {
	actor       whatsappform_usecase.Actor
	calls       []string
	summaries   []wf.FormSummary
	searched    string
	archived    bool
	detail      *whatsappform_usecase.FormDetail
	created     whatsappform_usecase.CreateInput
	savedDoc    *wf.Document
	savedBase   string
	checked     *wf.Document
	problems    wf.Problems
	published   whatsappform_usecase.PublishResult
	targets     []whatsappform_usecase.PublishTarget
	sendable    []whatsappform_usecase.SendableForm
	sendableQ   whatsappform_usecase.SendableQuery
	sent        whatsappform_usecase.SendInput
	responsesQ  wf.ResponseQuery
	responses   wf.Page[whatsappform_usecase.ResponseRow]
	stats       wf.Stats
	period      whatsappform_usecase.Period
	distributed []string
	workflowID  string
	err         error
	errOn       map[string]error
}

func (f *fakeForms) record(a whatsappform_usecase.Actor, call string) error {
	f.actor = a
	f.calls = append(f.calls, call)
	if err, ok := f.errOn[call]; ok {
		return err
	}
	return f.err
}

func (f *fakeForms) called(call string) bool {
	for _, c := range f.calls {
		if c == call {
			return true
		}
	}
	return false
}

func (f *fakeForms) List(_ context.Context, a whatsappform_usecase.Actor, search string, archived bool) ([]wf.FormSummary, error) {
	f.searched, f.archived = search, archived
	if err := f.record(a, "List"); err != nil {
		return nil, err
	}
	return f.summaries, nil
}

func (f *fakeForms) Get(_ context.Context, a whatsappform_usecase.Actor, formID string) (whatsappform_usecase.FormDetail, error) {
	if err := f.record(a, "Get"); err != nil {
		return whatsappform_usecase.FormDetail{}, err
	}
	if f.detail == nil || f.detail.Form.ID != formID {
		return whatsappform_usecase.FormDetail{}, wf.ErrFormNotFound
	}
	return *f.detail, nil
}

func (f *fakeForms) Create(_ context.Context, a whatsappform_usecase.Actor, in whatsappform_usecase.CreateInput) (whatsappform_usecase.FormDetail, error) {
	f.created = in
	if err := f.record(a, "Create"); err != nil {
		return whatsappform_usecase.FormDetail{}, err
	}
	return whatsappform_usecase.FormDetail{Form: wf.Form{ID: knownForm, Name: in.Name, DraftVersionID: "v-1"}, Draft: wf.Version{ID: "v-1", Number: 1}}, nil
}

func (f *fakeForms) SaveDraft(_ context.Context, a whatsappform_usecase.Actor, formID string, doc wf.Document, baseVersionID string) (wf.Version, error) {
	f.savedDoc, f.savedBase = &doc, baseVersionID
	if err := f.record(a, "SaveDraft"); err != nil {
		return wf.Version{}, err
	}
	return wf.Version{ID: "v-2", FormID: formID, Number: 2, Document: doc}, nil
}

func (f *fakeForms) Check(_ context.Context, a whatsappform_usecase.Actor, doc wf.Document) (wf.Problems, error) {
	f.checked = &doc
	if err := f.record(a, "Check"); err != nil {
		return nil, err
	}
	return f.problems, nil
}

func (f *fakeForms) PublishTargets(_ context.Context, a whatsappform_usecase.Actor) ([]whatsappform_usecase.PublishTarget, error) {
	return f.targets, nil
}

func (f *fakeForms) Publish(_ context.Context, a whatsappform_usecase.Actor, formID string, metaWABAIDs []string) (whatsappform_usecase.PublishResult, error) {
	if err := f.record(a, "Publish:"+formID+":"+strings.Join(metaWABAIDs, ",")); err != nil {
		return whatsappform_usecase.PublishResult{}, err
	}
	return f.published, nil
}

func (f *fakeForms) Sendable(_ context.Context, a whatsappform_usecase.Actor, q whatsappform_usecase.SendableQuery) ([]whatsappform_usecase.SendableForm, error) {
	f.sendableQ = q
	if err := f.record(a, "Sendable"); err != nil {
		return nil, err
	}
	return f.sendable, nil
}

func (f *fakeForms) SendInConversation(_ context.Context, a whatsappform_usecase.Actor, in whatsappform_usecase.SendInput) (wf.Send, error) {
	f.sent = in
	if err := f.record(a, "SendInConversation"); err != nil {
		return wf.Send{}, err
	}
	return wf.Send{ID: "send-1", FormID: in.FormID, Status: wf.SendSent}, nil
}

func (f *fakeForms) ListResponses(_ context.Context, a whatsappform_usecase.Actor, q wf.ResponseQuery) (wf.Page[whatsappform_usecase.ResponseRow], error) {
	f.responsesQ = q
	if err := f.record(a, "ListResponses"); err != nil {
		return wf.Page[whatsappform_usecase.ResponseRow]{}, err
	}
	normalized := q
	normalized.WorkspaceID, normalized.Scope = a.WorkspaceID, wf.ReadScope{Resolved: true}
	if err := normalized.Normalize(); err != nil {
		return wf.Page[whatsappform_usecase.ResponseRow]{}, err
	}
	return f.responses, nil
}

func (f *fakeForms) Stats(_ context.Context, a whatsappform_usecase.Actor, formID string, p whatsappform_usecase.Period) (wf.Stats, error) {
	f.period = p
	if err := f.record(a, "Stats:"+formID); err != nil {
		return wf.Stats{}, err
	}
	return f.stats, nil
}

func (f *fakeForms) Distribution(_ context.Context, a whatsappform_usecase.Actor, formID, fieldKey string, p whatsappform_usecase.Period) (map[string]int64, error) {
	f.distributed = append(f.distributed, fieldKey)
	if err := f.record(a, "Distribution:"+formID); err != nil {
		return nil, err
	}
	return map[string]int64{"o_aa": 7, "o_ab": 3}, nil
}

func (f *fakeForms) ScaffoldAutomation(_ context.Context, a whatsappform_usecase.Actor, formID string) (string, error) {
	if err := f.record(a, "ScaffoldAutomation:"+formID); err != nil {
		return "", err
	}
	return f.workflowID, nil
}

type fakeReports struct {
	in  report_usecase.CreateInput
	err error
}

func (f *fakeReports) Create(in report_usecase.CreateInput) (*report.Job, error) {
	f.in = in
	if f.err != nil {
		return nil, f.err
	}
	return &report.Job{ID: "report-1", Kind: in.Kind, Format: in.Format, Status: report.StatusQueued}, nil
}

type fakeEntries struct {
	entry *conversation.InboxEntry
}

func (f fakeEntries) LookupEntry(_ conversation.Viewer, entryID string, entryType shared.EntryType) (*conversation.InboxEntry, error) {
	if f.entry == nil || f.entry.EntryID != entryID || f.entry.EntryType != string(entryType) {
		return nil, conversation.ErrUnauthorized
	}
	copied := *f.entry
	return &copied, nil
}

func openEntry() *conversation.InboxEntry {
	return &conversation.InboxEntry{EntryID: knownEntry, EntryType: "whatsapp", LeadName: "Maria", LeadNumber: "5584994409624", WindowOpen: true, BusinessPhoneID: knownPhone}
}

func formsWith(detail whatsappform_usecase.FormDetail) *fakeForms {
	return &fakeForms{detail: &detail}
}

func deps(forms *fakeForms) FormDeps {
	return FormDeps{Forms: forms, Reports: &fakeReports{}, Entries: fakeEntries{entry: openEntry()}}
}

func at(day string) time.Time {
	t, err := time.Parse("2006-01-02 15:04", day)
	if err != nil {
		panic(err)
	}
	return t
}
