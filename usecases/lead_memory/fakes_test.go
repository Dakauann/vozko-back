package lead_memory_usecase

import (
	"sort"
	"strings"
	"time"

	"vozko/domain/agent"
	ce "vozko/domain/conversation_event"
	leadmemory "vozko/domain/lead_memory"
	"vozko/domain/user"
)

// fakeRepo is an in-memory leadmemory.Repository honoring the same contracts
// the Postgres one does: workspace scoping, soft-delete invisibility, dedup by
// normalized content, prefix ambiguity.
type fakeRepo struct {
	rows    map[string]*leadmemory.LeadMemory
	deleted map[string]bool
	seq     int

	failWith error // when set, every method fails with it
}

func newFakeRepo() *fakeRepo {
	return &fakeRepo{rows: map[string]*leadmemory.LeadMemory{}, deleted: map[string]bool{}}
}

func (f *fakeRepo) Create(m *leadmemory.LeadMemory) error {
	if f.failWith != nil {
		return f.failWith
	}
	for _, r := range f.rows {
		if f.deleted[r.ID] {
			continue
		}
		if r.WorkspaceID == m.WorkspaceID && r.LeadID == m.LeadID &&
			leadmemory.NormalizeContent(r.Content) == leadmemory.NormalizeContent(m.Content) {
			return leadmemory.ErrDuplicate
		}
	}
	if m.ID == "" {
		// Deterministic 36-char ids with distinct 8-char prefixes.
		f.seq++
		m.ID = strings.Replace(
			"0000000#-0000-4000-8000-000000000000", "#", string(rune('0'+f.seq)), 1)
	}
	m.CreatedAt = time.Date(2026, 8, 13, 12, 0, 0, 0, time.UTC).Add(time.Duration(f.seq) * time.Minute)
	m.UpdatedAt = m.CreatedAt
	f.rows[m.ID] = m
	return nil
}

func (f *fakeRepo) FindByID(workspaceID, id string) (*leadmemory.LeadMemory, error) {
	if f.failWith != nil {
		return nil, f.failWith
	}
	m, ok := f.rows[id]
	if !ok || f.deleted[id] || m.WorkspaceID != workspaceID {
		return nil, leadmemory.ErrNotFound
	}
	return m, nil
}

func (f *fakeRepo) FindByIDPrefix(workspaceID, leadID, prefix string) (*leadmemory.LeadMemory, error) {
	if f.failWith != nil {
		return nil, f.failWith
	}
	var found []*leadmemory.LeadMemory
	for _, m := range f.rows {
		if f.deleted[m.ID] || m.WorkspaceID != workspaceID || m.LeadID != leadID {
			continue
		}
		if strings.HasPrefix(m.ID, strings.ToLower(prefix)) {
			found = append(found, m)
		}
	}
	switch len(found) {
	case 0:
		return nil, leadmemory.ErrNotFound
	case 1:
		return found[0], nil
	default:
		return nil, leadmemory.ErrAmbiguousID
	}
}

func (f *fakeRepo) ListByLead(workspaceID, leadID string, q leadmemory.ListQuery) ([]*leadmemory.LeadMemory, int64, error) {
	if f.failWith != nil {
		return nil, 0, f.failWith
	}
	var out []*leadmemory.LeadMemory
	for _, m := range f.rows {
		if f.deleted[m.ID] || m.WorkspaceID != workspaceID || m.LeadID != leadID {
			continue
		}
		if q.Category != nil && m.Category != *q.Category {
			continue
		}
		out = append(out, m)
	}
	sort.Slice(out, func(i, j int) bool { return out[i].CreatedAt.After(out[j].CreatedAt) })
	total := int64(len(out))
	limit := q.Limit
	if limit < 1 || limit > leadmemory.MaxListLimit {
		limit = leadmemory.DefaultListLimit
	}
	if len(out) > limit {
		out = out[:limit]
	}
	return out, total, nil
}

func (f *fakeRepo) CountByLead(workspaceID, leadID string) (int64, error) {
	if f.failWith != nil {
		return 0, f.failWith
	}
	_, total, err := f.ListByLead(workspaceID, leadID, leadmemory.ListQuery{Limit: leadmemory.MaxListLimit})
	return total, err
}

func (f *fakeRepo) FindByNormalizedContent(workspaceID, leadID, contentNorm string) (*leadmemory.LeadMemory, error) {
	if f.failWith != nil {
		return nil, f.failWith
	}
	for _, m := range f.rows {
		if f.deleted[m.ID] || m.WorkspaceID != workspaceID || m.LeadID != leadID {
			continue
		}
		if leadmemory.NormalizeContent(m.Content) == contentNorm {
			return m, nil
		}
	}
	return nil, leadmemory.ErrNotFound
}

func (f *fakeRepo) Update(m *leadmemory.LeadMemory) error {
	if f.failWith != nil {
		return f.failWith
	}
	existing, ok := f.rows[m.ID]
	if !ok || f.deleted[m.ID] || existing.WorkspaceID != m.WorkspaceID {
		return leadmemory.ErrNotFound
	}
	f.rows[m.ID] = m
	return nil
}

func (f *fakeRepo) SoftDelete(workspaceID, id string) error {
	if f.failWith != nil {
		return f.failWith
	}
	m, ok := f.rows[id]
	if !ok || f.deleted[id] || m.WorkspaceID != workspaceID {
		return leadmemory.ErrNotFound
	}
	f.deleted[id] = true
	return nil
}

var _ leadmemory.Repository = (*fakeRepo)(nil)

// fakeTimeline captures emitted events.
type fakeTimeline struct {
	events []*ce.ConversationEvent
}

func (f *fakeTimeline) ConversationEvent(ev *ce.ConversationEvent) {
	f.events = append(f.events, ev)
}

type fakeAgentFinder struct {
	agents []*agent.Agent
	err    error
}

func (f *fakeAgentFinder) FindByIDs(ids []string) ([]*agent.Agent, error) {
	return f.agents, f.err
}

type fakeUserFinder struct {
	users []*user.User
	err   error
}

func (f *fakeUserFinder) FindByIDs(ids []string) ([]*user.User, error) {
	return f.users, f.err
}
