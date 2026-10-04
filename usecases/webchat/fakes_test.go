package webchat

import (
	"context"
	"errors"
	"sync"
	"time"

	"github.com/google/uuid"

	"vozko/domain/agent"
	"vozko/domain/conversation"
	ia "vozko/domain/inbox_assignment"
	"vozko/domain/lead"
	"vozko/domain/pipeline"
	"vozko/domain/shared"
	wcdomain "vozko/domain/webchat"
	"vozko/domain/workflow"
	wd "vozko/domain/workspace/workspace_department"
	conversation_usecase "vozko/usecases/conversation"
)

type fakeWidgets struct{ byID map[string]*wcdomain.Widget }

func (f *fakeWidgets) Create(_ context.Context, w *wcdomain.Widget) error {
	w.ID = uuid.NewString()
	f.byID[w.ID] = w
	return nil
}
func (f *fakeWidgets) Update(_ context.Context, w *wcdomain.Widget) error {
	f.byID[w.ID] = w
	return nil
}
func (f *fakeWidgets) FindByID(_ context.Context, ws, id string) (*wcdomain.Widget, error) {
	if w, ok := f.byID[id]; ok && w.WorkspaceID == ws {
		copied := *w
		return &copied, nil
	}
	return nil, wcdomain.ErrWidgetNotFound
}
func (f *fakeWidgets) FindByIDUnscoped(_ context.Context, id string) (*wcdomain.Widget, error) {
	if w, ok := f.byID[id]; ok {
		return w, nil
	}
	return nil, wcdomain.ErrWidgetNotFound
}
func (f *fakeWidgets) FindByPublicKey(_ context.Context, key string) (*wcdomain.Widget, error) {
	for _, w := range f.byID {
		if w.PublicKey == key {
			return w, nil
		}
	}
	return nil, wcdomain.ErrWidgetNotFound
}
func (f *fakeWidgets) ListByWorkspace(context.Context, wcdomain.ListWidgetsInput) (*shared.PaginatedResult[*wcdomain.Widget], error) {
	return nil, nil
}
func (f *fakeWidgets) Delete(context.Context, string, string) error { return nil }

type fakeVisitors struct{ byID map[string]*wcdomain.Visitor }

func (f *fakeVisitors) Create(_ context.Context, v *wcdomain.Visitor) error {
	v.ID = uuid.NewString()
	f.byID[v.ID] = v
	return nil
}
func (f *fakeVisitors) FindByID(_ context.Context, id string) (*wcdomain.Visitor, error) {
	if v, ok := f.byID[id]; ok {
		return v, nil
	}
	return nil, wcdomain.ErrVisitorNotFound
}
func (f *fakeVisitors) FindByIDs(context.Context, []string) ([]*wcdomain.Visitor, error) {
	return nil, nil
}
func (f *fakeVisitors) FindByExternalID(_ context.Context, widgetID, externalID string) (*wcdomain.Visitor, error) {
	for _, v := range f.byID {
		if v.WidgetID == widgetID && v.ExternalID != nil && *v.ExternalID == externalID {
			return v, nil
		}
	}
	return nil, wcdomain.ErrVisitorNotFound
}
func (f *fakeVisitors) ApplyIdentity(_ context.Context, id string, c wcdomain.IdentityClaims) error {
	v := f.byID[id]
	v.IdentityVerified, v.Name, v.Email, v.Phone = true, c.Name, c.Email, c.Phone
	return nil
}
func (f *fakeVisitors) SaveIntake(_ context.Context, id string, in wcdomain.Intake, leadID *string, at time.Time) error {
	v := f.byID[id]
	v.Name, v.Email, v.Phone, v.LeadID, v.IntakeCompletedAt = in.Name, in.Email, in.Phone, leadID, &at
	return nil
}
func (f *fakeVisitors) Touch(context.Context, string, wcdomain.VisitorSighting) error { return nil }
func (f *fakeVisitors) SetBlocked(_ context.Context, id string, blocked bool, _ time.Time) error {
	f.byID[id].Blocked = blocked
	return nil
}

type fakeConversations struct {
	byID map[string]*wcdomain.Conversation
}

func (f *fakeConversations) FindOrCreate(_ context.Context, in wcdomain.FindOrCreateConversationInput) (*wcdomain.Conversation, error) {
	for _, c := range f.byID {
		if c.WidgetID == in.WidgetID && c.VisitorID == in.VisitorID {
			return c, nil
		}
	}
	c := &wcdomain.Conversation{ID: uuid.NewString(), WorkspaceID: in.WorkspaceID, WidgetID: in.WidgetID, VisitorID: in.VisitorID}
	f.byID[c.ID] = c
	return c, nil
}
func (f *fakeConversations) FindByID(_ context.Context, id string) (*wcdomain.Conversation, error) {
	if c, ok := f.byID[id]; ok {
		return c, nil
	}
	return nil, wcdomain.ErrConversationNotFound
}
func (f *fakeConversations) FindByVisitor(_ context.Context, widgetID, visitorID string) (*wcdomain.Conversation, error) {
	for _, c := range f.byID {
		if c.WidgetID == widgetID && c.VisitorID == visitorID {
			return c, nil
		}
	}
	return nil, wcdomain.ErrConversationNotFound
}
func (f *fakeConversations) WorkspaceIDForEntry(context.Context, string) (string, error) {
	return "", nil
}
func (f *fakeConversations) DepartmentIDForEntry(context.Context, string) (string, error) {
	return "", nil
}
func (f *fakeConversations) ListEntryIDsByWorkspace(context.Context, string) ([]string, error) {
	return nil, nil
}
func (f *fakeConversations) RecordInbound(context.Context, string, time.Time) error  { return nil }
func (f *fakeConversations) RecordOutbound(context.Context, string, time.Time) error { return nil }
func (f *fakeConversations) SetStatus(context.Context, string, conversation.StatusWrite) error {
	return nil
}
func (f *fakeConversations) SetAutomationEnabled(context.Context, string, *bool) error { return nil }
func (f *fakeConversations) StatusForEntry(context.Context, string) (string, error)    { return "", nil }
func (f *fakeConversations) CountByStatus(context.Context, string, string) (map[string]int64, error) {
	return nil, nil
}
func (f *fakeConversations) SetPendingOptions(_ context.Context, id string, options []wcdomain.Option) error {
	f.byID[id].PendingOptions = options
	return nil
}

type fakeOneShot struct {
	mu   sync.Mutex
	keys map[string]bool
	err  error
}

func (f *fakeOneShot) SetNX(key, _ string, _ time.Duration) (bool, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	if f.err != nil {
		return false, f.err
	}
	if f.keys[key] {
		return false, nil
	}
	f.keys[key] = true
	return true, nil
}
func (f *fakeOneShot) Del(keys ...string) error {
	for _, k := range keys {
		delete(f.keys, k)
	}
	return nil
}

type fakeLimiter struct {
	allowed bool
	err     error
}

func (f fakeLimiter) Allow(string) (bool, time.Duration, error) { return f.allowed, 0, f.err }

func openLimits() Limits {
	ok := fakeLimiter{allowed: true}
	return Limits{ok, ok, ok, ok, ok, ok, ok, ok}
}

type fakeTranscript struct {
	records []conversation.MessageHistoryRecord
	err     error
}

func (f *fakeTranscript) Record(_ context.Context, r conversation.MessageHistoryRecord) error {
	if f.err != nil {
		return f.err
	}
	f.records = append(f.records, r)
	return nil
}

type fakeAssignments struct {
	ensured  []string
	handoffs []ia.RouletteHandOff
}

func (f *fakeAssignments) EnsureAssignment(entryID, _, _ string) string {
	f.ensured = append(f.ensured, entryID)
	return ""
}
func (f *fakeAssignments) HandOffToRoulette(in ia.RouletteHandOff) (string, error) {
	f.handoffs = append(f.handoffs, in)
	return "", nil
}

type fakeAutomation struct {
	inputs []conversation_usecase.InboundAutomationInput
}

func (f *fakeAutomation) Dispatch(_ context.Context, in conversation_usecase.InboundAutomationInput) {
	f.inputs = append(f.inputs, in)
}

type fakeEvents struct{ events []wcdomain.VisitorEvent }

func (f *fakeEvents) Publish(_ context.Context, e wcdomain.VisitorEvent) error {
	f.events = append(f.events, e)
	return nil
}

type fakeOperators struct{ updates []string }

func (f *fakeOperators) BroadcastTyping(string, string, string, bool) {}
func (f *fakeOperators) BroadcastEntryUpdate(entryID, _ string, _ *conversation.Message) {
	f.updates = append(f.updates, entryID)
}

type fakeLeads struct{ calls []string }

func (f *fakeLeads) FindOrCreate(_, number string, _ lead.LeadUpdate) (*lead.Lead, bool, error) {
	f.calls = append(f.calls, number)
	return &lead.Lead{ID: "lead-" + number}, true, nil
}

type fakeMessages struct{ rows []*conversation.Message }

func (f fakeMessages) ListByEntryPaginated(conversation.ListMessagesInput) ([]*conversation.Message, error) {
	return f.rows, nil
}

type noPresenter struct{}

func (noPresenter) PresentMessages(string, shared.EntryType, []*conversation.Message) {}

type fakeAgents map[string]*agent.Agent

func (f fakeAgents) FindByID(id string) (*agent.Agent, error) {
	if a, ok := f[id]; ok {
		return a, nil
	}
	return nil, errors.New("agent not found")
}

type fakeWorkflows map[string]*workflow.Workflow

func (f fakeWorkflows) FindByID(id string) (*workflow.Workflow, error) {
	if w, ok := f[id]; ok {
		return w, nil
	}
	return nil, errors.New("workflow not found")
}

type fakePipelines map[string]*pipeline.Pipeline

func (f fakePipelines) GetByID(_, id string) (*pipeline.Pipeline, error) {
	if p, ok := f[id]; ok {
		return p, nil
	}
	return nil, errors.New("pipeline not found")
}

type fakeDepartments map[string]*wd.Department

func (f fakeDepartments) GetDepartmentByID(id string) (*wd.Department, error) {
	if d, ok := f[id]; ok {
		return d, nil
	}
	return nil, errors.New("department not found")
}

type fakeAccess bool

func (f fakeAccess) CanAccessEntry(string, string, string, string, bool) bool { return bool(f) }

type fakeMedia struct {
	stored []conversation.StoreMediaInput
}

func (f *fakeMedia) Store(in conversation.StoreMediaInput) (*conversation.ConversationMedia, error) {
	f.stored = append(f.stored, in)
	return &conversation.ConversationMedia{ID: in.ID, URL: "https://cdn.example/" + in.Key, MimeType: in.MimeType, OriginalFilename: in.OriginalFilename}, nil
}
