package personalinbox_usecase

import (
	"context"
	"sync"

	"vozko/domain/personalinbox"
	"vozko/domain/shared"
)

type markKey struct {
	user  string
	entry shared.EntryRef
}

type memoryMarks struct {
	mu    sync.Mutex
	marks map[markKey]personalinbox.Mark
	finds int
}

func newMemoryMarks(seed ...personalinbox.Mark) *memoryMarks {
	m := &memoryMarks{marks: map[markKey]personalinbox.Mark{}}
	for _, mark := range seed {
		m.marks[markKey{mark.UserID, mark.Entry()}] = mark
	}
	return m
}

func (m *memoryMarks) Find(_ context.Context, userID string, entry shared.EntryRef) (personalinbox.Mark, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.finds++
	if mark, ok := m.marks[markKey{userID, entry}]; ok {
		return mark, nil
	}
	return personalinbox.NewMark(userID, entry)
}

func (m *memoryMarks) Change(_ context.Context, userID string, entry shared.EntryRef, change personalinbox.Change) (personalinbox.Mark, personalinbox.Mark, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	before, ok := m.marks[markKey{userID, entry}]
	if !ok {
		var err error
		if before, err = personalinbox.NewMark(userID, entry); err != nil {
			return personalinbox.Mark{}, personalinbox.Mark{}, err
		}
	}
	after, err := change(before)
	if err != nil {
		return personalinbox.Mark{}, personalinbox.Mark{}, err
	}
	if after.Empty() {
		delete(m.marks, markKey{userID, entry})
	} else {
		m.marks[markKey{userID, entry}] = after
	}
	return before, after, nil
}

func (m *memoryMarks) ForEntries(_ context.Context, userID string, entries []shared.EntryRef) ([]personalinbox.Mark, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	var out []personalinbox.Mark
	for _, e := range entries {
		if mark, ok := m.marks[markKey{userID, e}]; ok {
			out = append(out, mark)
		}
	}
	return out, nil
}

func (m *memoryMarks) get(user string, entry shared.EntryRef) (personalinbox.Mark, bool) {
	m.mu.Lock()
	defer m.mu.Unlock()
	mark, ok := m.marks[markKey{user, entry}]
	return mark, ok
}

type access struct {
	readers map[string]bool
	sees    map[markKey]bool
}

func (a access) HasWorkspacePermission(userID, workspaceID, resource, action string, isSystemAdmin bool) bool {
	if isSystemAdmin {
		return true
	}
	return workspaceID == homeWorkspace && resource == "conversations" && action == "read" && a.readers[userID]
}

func (a access) CanAccessEntry(userID, workspaceID, entryID, entryType string, isAdmin bool) bool {
	return workspaceID == homeWorkspace && a.sees[markKey{userID, shared.EntryRef{EntryID: entryID, EntryType: shared.EntryType(entryType)}}]
}

type announced struct {
	workspaceID string
	mark        personalinbox.Mark
}

type announcer struct {
	mu   sync.Mutex
	sent []announced
}

func (a *announcer) AnnouncePersonalMark(workspaceID string, mark personalinbox.Mark) {
	a.mu.Lock()
	defer a.mu.Unlock()
	a.sent = append(a.sent, announced{workspaceID, mark})
}

func (a *announcer) to(user string) []announced {
	a.mu.Lock()
	defer a.mu.Unlock()
	var out []announced
	for _, s := range a.sent {
		if s.mark.UserID == user {
			out = append(out, s)
		}
	}
	return out
}
