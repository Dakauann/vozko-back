package audiencetest

import (
	"context"
	"sort"
	"strings"
	"sync"
	"time"

	"vozko/domain/campaignaudience"
)

type Lists struct {
	mu    sync.Mutex
	lists map[string]campaignaudience.List
	rows  map[string][]campaignaudience.Row
}

func NewLists() *Lists {
	return &Lists{lists: map[string]campaignaudience.List{}, rows: map[string][]campaignaudience.Row{}}
}

func (m *Lists) Create(_ context.Context, l campaignaudience.List, rows []campaignaudience.Row) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.lists[l.ID] = l
	m.rows[l.ID] = append([]campaignaudience.Row(nil), rows...)
	return nil
}

func (m *Lists) Get(_ context.Context, workspaceID, id string) (*campaignaudience.List, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	l, ok := m.lists[id]
	if !ok || l.WorkspaceID != workspaceID {
		return nil, campaignaudience.ErrListNotFound
	}
	return &l, nil
}

func (m *Lists) Save(_ context.Context, l campaignaudience.List) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.lists[l.ID] = l
	return nil
}

func (m *Lists) Delete(_ context.Context, workspaceID, id string) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	if l, ok := m.lists[id]; ok && l.WorkspaceID == workspaceID {
		delete(m.lists, id)
		delete(m.rows, id)
	}
	return nil
}

func (m *Lists) ordered(listID string) []campaignaudience.Row {
	rows := append([]campaignaudience.Row(nil), m.rows[listID]...)
	sort.SliceStable(rows, func(i, j int) bool {
		if rows[i].Line != rows[j].Line {
			return rows[i].Line < rows[j].Line
		}
		return rows[i].ID < rows[j].ID
	})
	return rows
}

func (m *Lists) RowsAfter(_ context.Context, listID string, after campaignaudience.RowCursor, limit int) ([]campaignaudience.Row, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	var out []campaignaudience.Row
	for _, r := range m.ordered(listID) {
		if r.Line < after.Line || (r.Line == after.Line && r.ID <= after.ID) {
			continue
		}
		if len(out) == limit {
			break
		}
		out = append(out, r)
	}
	return out, nil
}

func (m *Lists) SearchRows(_ context.Context, listID string, q campaignaudience.RowQuery) ([]campaignaudience.Row, int, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	var matched []campaignaudience.Row
	for _, r := range m.ordered(listID) {
		term := strings.ToLower(strings.TrimSpace(q.Search))
		if term == "" || strings.Contains(strings.ToLower(r.Contact), term) || strings.Contains(strings.ToLower(r.Name), term) {
			matched = append(matched, r)
		}
	}
	start := min(q.Offset, len(matched))
	end := min(start+q.Limit, len(matched))
	return matched[start:end], len(matched), nil
}

func (m *Lists) CountRows(_ context.Context, listID string) (int, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	return len(m.rows[listID]), nil
}

func (m *Lists) AddRow(_ context.Context, row campaignaudience.Row) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	for _, r := range m.rows[row.ListID] {
		if r.Contact == row.Contact {
			return campaignaudience.ErrRowDuplicate
		}
	}
	m.rows[row.ListID] = append(m.rows[row.ListID], row)
	return nil
}

func (m *Lists) UpdateRow(_ context.Context, row campaignaudience.Row) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	rows := m.rows[row.ListID]
	for _, r := range rows {
		if r.ID != row.ID && r.Contact == row.Contact {
			return campaignaudience.ErrRowDuplicate
		}
	}
	for i, r := range rows {
		if r.ID == row.ID {
			rows[i] = row
			return nil
		}
	}
	return campaignaudience.ErrRowNotFound
}

func (m *Lists) DeleteRow(_ context.Context, listID, rowID string) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	rows := m.rows[listID]
	for i, r := range rows {
		if r.ID == rowID {
			m.rows[listID] = append(rows[:i], rows[i+1:]...)
			return nil
		}
	}
	return campaignaudience.ErrRowNotFound
}

func (m *Lists) DeleteExpired(_ context.Context, now time.Time, limit int) (int, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	n := 0
	for id, l := range m.lists {
		if l.Expired(now) && n < limit {
			delete(m.lists, id)
			delete(m.rows, id)
			n++
		}
	}
	return n, nil
}
func (m *Lists) Row(_ context.Context, listID, rowID string) (campaignaudience.Row, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	for _, r := range m.rows[listID] {
		if r.ID == rowID {
			return r, nil
		}
	}
	return campaignaudience.Row{}, campaignaudience.ErrRowNotFound
}

func (m *Lists) Len() int {
	m.mu.Lock()
	defer m.mu.Unlock()
	return len(m.lists)
}
