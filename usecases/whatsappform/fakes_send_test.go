package whatsappform_usecase

import (
	"context"
	"sort"
	"sync"

	"vozko/domain/conversation"
	"vozko/domain/lead"
	"vozko/domain/shared"
	wf "vozko/domain/whatsappform"
	lead_usecase "vozko/usecases/lead"
)

type sendsRepo struct {
	mu   sync.Mutex
	rows map[string]wf.Send
}

func newSendsRepo() *sendsRepo { return &sendsRepo{rows: map[string]wf.Send{}} }

func (r *sendsRepo) Insert(_ context.Context, s wf.Send) error {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.rows[s.ID] = s
	return nil
}

func (r *sendsRepo) Update(_ context.Context, s wf.Send) error {
	r.mu.Lock()
	defer r.mu.Unlock()
	if _, ok := r.rows[s.ID]; !ok {
		return wf.ErrSendNotFound
	}
	r.rows[s.ID] = s
	return nil
}

func (r *sendsRepo) FindByToken(_ context.Context, token string) (*wf.Send, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	for _, s := range r.rows {
		if s.Token == token {
			return &s, nil
		}
	}
	return nil, wf.ErrSendNotFound
}

func (r *sendsRepo) FindByMessageID(_ context.Context, messageID string) (*wf.Send, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	for _, s := range r.rows {
		if s.MessageID != "" && s.MessageID == messageID {
			return &s, nil
		}
	}
	return nil, wf.ErrSendNotFound
}

func (r *sendsRepo) List(_ context.Context, q wf.SendQuery) (wf.Page[wf.Send], error) {
	if !q.Scope.Resolved {
		return wf.Page[wf.Send]{}, wf.ErrForbidden
	}
	r.mu.Lock()
	defer r.mu.Unlock()
	var out []wf.Send
	for _, s := range r.rows {
		if s.WorkspaceID == q.WorkspaceID && s.FormID == q.FormID {
			out = append(out, s)
		}
	}
	sort.Slice(out, func(i, j int) bool { return out[i].ID > out[j].ID })
	return wf.Page[wf.Send]{Items: out}, nil
}

func (r *sendsRepo) all() []wf.Send {
	r.mu.Lock()
	defer r.mu.Unlock()
	var out []wf.Send
	for _, s := range r.rows {
		out = append(out, s)
	}
	sort.Slice(out, func(i, j int) bool { return out[i].ID < out[j].ID })
	return out
}

type responsesRepo struct {
	mu   sync.Mutex
	rows map[string]wf.Response
}

func newResponsesRepo() *responsesRepo { return &responsesRepo{rows: map[string]wf.Response{}} }

func (r *responsesRepo) Insert(_ context.Context, resp wf.Response) (wf.Response, bool, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	for _, existing := range r.rows {
		if existing.InboundMessageID == resp.InboundMessageID {
			return existing, false, nil
		}
	}
	r.rows[resp.ID] = resp
	return resp, true, nil
}

func (r *responsesRepo) SaveApply(_ context.Context, workspaceID, id string, status wf.ApplyStatus, report wf.ApplyReport) error {
	r.mu.Lock()
	defer r.mu.Unlock()
	resp, ok := r.rows[id]
	if !ok || resp.WorkspaceID != workspaceID {
		return wf.ErrResponseNotFound
	}
	resp.ApplyStatus, resp.Report = status, report
	r.rows[id] = resp
	return nil
}

func (r *responsesRepo) Get(_ context.Context, workspaceID, id string, scope wf.ReadScope) (*wf.Response, error) {
	if !scope.Resolved {
		return nil, wf.ErrForbidden
	}
	r.mu.Lock()
	defer r.mu.Unlock()
	resp, ok := r.rows[id]
	if !ok || resp.WorkspaceID != workspaceID || (scope.Restrict && resp.EntryID != scope.AssigneeUserID) {
		return nil, wf.ErrResponseNotFound
	}
	return &resp, nil
}

func (r *responsesRepo) List(_ context.Context, q wf.ResponseQuery) (wf.Page[wf.Response], error) {
	if !q.Scope.Resolved {
		return wf.Page[wf.Response]{}, wf.ErrForbidden
	}
	r.mu.Lock()
	defer r.mu.Unlock()
	var out []wf.Response
	for _, resp := range r.rows {
		if resp.WorkspaceID == q.WorkspaceID && resp.FormID == q.FormID {
			out = append(out, resp)
		}
	}
	sort.Slice(out, func(i, j int) bool { return out[i].ID > out[j].ID })
	return wf.Page[wf.Response]{Items: out}, nil
}

func (r *responsesRepo) Delete(_ context.Context, workspaceID, id string) error {
	r.mu.Lock()
	defer r.mu.Unlock()
	if resp, ok := r.rows[id]; !ok || resp.WorkspaceID != workspaceID {
		return wf.ErrResponseNotFound
	}
	delete(r.rows, id)
	return nil
}

func (r *responsesRepo) Stats(_ context.Context, q wf.StatsQuery) (wf.Stats, error) {
	if !q.Scope.Resolved {
		return wf.Stats{}, wf.ErrForbidden
	}
	r.mu.Lock()
	defer r.mu.Unlock()
	return wf.Stats{Responses: int64(len(r.rows))}, nil
}

func (r *responsesRepo) Distribution(_ context.Context, q wf.StatsQuery, key string) (map[string]int64, error) {
	if !q.Scope.Resolved {
		return nil, wf.ErrForbidden
	}
	return map[string]int64{}, nil
}

func (r *responsesRepo) all() []wf.Response {
	r.mu.Lock()
	defer r.mu.Unlock()
	var out []wf.Response
	for _, resp := range r.rows {
		out = append(out, resp)
	}
	sort.Slice(out, func(i, j int) bool { return out[i].ID < out[j].ID })
	return out
}

type sentFlow struct {
	phoneID string
	input   conversation.SendFlowMessageInput
}

type flowSender struct {
	mu   sync.Mutex
	sent []sentFlow
	err  error
}

func (f *flowSender) SendFlowMessage(_ context.Context, phoneID string, in conversation.SendFlowMessageInput) (string, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	if f.err != nil {
		return "", f.err
	}
	f.sent = append(f.sent, sentFlow{phoneID: phoneID, input: in})
	return "wamid.out-" + in.FlowToken, nil
}

type targets struct {
	byEntry map[string]ConversationTarget
	denied  map[string]bool
	scope   wf.ReadScope
	scopeOK bool
}

func newTargets() *targets {
	return &targets{
		byEntry: map[string]ConversationTarget{
			"entry-1":       {EntryID: "entry-1", EntryType: string(shared.EntryTypeWhatsApp), LeadID: "lead-1", BusinessPhoneID: "p1", ContactNumber: "5511987654321", WindowOpen: true},
			"entry-closed":  {EntryID: "entry-closed", EntryType: string(shared.EntryTypeWhatsApp), LeadID: "lead-2", BusinessPhoneID: "p1", ContactNumber: "5511911112222"},
			"entry-foreign": {EntryID: "entry-foreign", EntryType: string(shared.EntryTypeWhatsApp), LeadID: "lead-3", BusinessPhoneID: "p4", ContactNumber: "5511933334444", WindowOpen: true},
		},
		denied:  map[string]bool{},
		scope:   wf.ReadScope{Resolved: true},
		scopeOK: true,
	}
}

func (t *targets) Target(_ context.Context, workspaceID, entryID string) (ConversationTarget, error) {
	target, ok := t.byEntry[entryID]
	if !ok {
		return ConversationTarget{}, wf.ErrChannelUnsupported
	}
	return target, nil
}

func (t *targets) CanAccessEntry(userID, workspaceID, entryID, entryType string, isAdmin bool) bool {
	return workspaceID == ws && !t.denied[entryID]
}

func (t *targets) ReadScope(userID, workspaceID string, isAdmin bool) (wf.ReadScope, bool) {
	return t.scope, t.scopeOK
}

type resumedRun struct {
	runID string
	data  map[string]any
}

type effects struct {
	mu         sync.Mutex
	outbound   []OutboundForm
	profiles   []lead_usecase.ProfileUpdate
	profileOut lead_usecase.ProfileResult
	profileErr error
	consents   []lead.ConsentChange
	stored     []StoredFile
	mediaErr   error
	resumed    []resumedRun
	triggered  []FormTrigger
	names      map[string]string
}

func newEffects() *effects {
	return &effects{names: map[string]string{"lead-1": "Ana Souza"}}
}

func (e *effects) RecordFormMessage(_ context.Context, out OutboundForm) error {
	e.mu.Lock()
	defer e.mu.Unlock()
	e.outbound = append(e.outbound, out)
	return nil
}

func (e *effects) Update(_ context.Context, in lead_usecase.ProfileUpdate) (lead_usecase.ProfileResult, error) {
	e.mu.Lock()
	defer e.mu.Unlock()
	e.profiles = append(e.profiles, in)
	return e.profileOut, e.profileErr
}

func (e *effects) Record(_ context.Context, workspaceID, leadID string, change lead.ConsentChange) (lead_usecase.ConsentOutcome, error) {
	e.mu.Lock()
	defer e.mu.Unlock()
	e.consents = append(e.consents, change)
	return lead_usecase.ConsentOutcome{Changed: true, EventID: "consent-1"}, nil
}

func (e *effects) StoreFormFile(_ context.Context, f StoredFile) (string, error) {
	e.mu.Lock()
	defer e.mu.Unlock()
	if e.mediaErr != nil {
		return "", e.mediaErr
	}
	e.stored = append(e.stored, f)
	return "media-" + f.File.ID, nil
}

func (e *effects) ResumeForm(_ context.Context, runID string, data map[string]any) error {
	e.mu.Lock()
	defer e.mu.Unlock()
	e.resumed = append(e.resumed, resumedRun{runID: runID, data: data})
	return nil
}

func (e *effects) FormSubmitted(_ context.Context, in FormTrigger) error {
	e.mu.Lock()
	defer e.mu.Unlock()
	e.triggered = append(e.triggered, in)
	return nil
}

func (e *effects) Names(_ context.Context, workspaceID string, ids []string) (map[string]string, error) {
	out := map[string]string{}
	for _, id := range ids {
		if name, ok := e.names[id]; ok {
			out[id] = name
		}
	}
	return out, nil
}
