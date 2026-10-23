package conversationnote_usecase

import (
	"context"
	"errors"
	"sort"
	"strings"
	"sync"
	"time"

	"vozko/domain/conversation"
	"vozko/domain/conversationnote"
	"vozko/domain/notification"
	"vozko/domain/shared"
	"vozko/domain/user"
)

type memoryNotes struct {
	mu    sync.Mutex
	notes map[string]conversationnote.Note
	err   error
}

func newMemoryNotes() *memoryNotes {
	return &memoryNotes{notes: map[string]conversationnote.Note{}}
}

func (m *memoryNotes) Create(_ context.Context, note conversationnote.Note) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	if m.err != nil {
		return m.err
	}
	m.notes[note.ID] = note
	return nil
}

func (m *memoryNotes) Find(_ context.Context, workspaceID, id string) (conversationnote.Note, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	note, ok := m.notes[id]
	if !ok || note.WorkspaceID != workspaceID || note.DeletedAt != nil {
		return conversationnote.Note{}, conversationnote.ErrNotFound
	}
	return note, nil
}

func (m *memoryNotes) ListByEntry(_ context.Context, q conversationnote.PageQuery) ([]conversationnote.Note, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	var out []conversationnote.Note
	for _, note := range m.notes {
		if note.WorkspaceID != q.WorkspaceID || note.Entry() != q.Entry || note.DeletedAt != nil {
			continue
		}
		if q.Before != nil && !note.CreatedAt.Before(*q.Before) {
			continue
		}
		out = append(out, note)
	}
	sort.Slice(out, func(i, j int) bool { return out[i].CreatedAt.After(out[j].CreatedAt) })
	if len(out) > q.Limit {
		out = out[:q.Limit]
	}
	return out, nil
}

func (m *memoryNotes) MarkDeleted(_ context.Context, workspaceID, id string, at time.Time) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	note, ok := m.notes[id]
	if !ok || note.WorkspaceID != workspaceID || note.DeletedAt != nil {
		return conversationnote.ErrNotFound
	}
	note.DeletedAt = &at
	m.notes[id] = note
	return nil
}

type fakeAccess struct {
	readers   map[string]bool
	seers     map[string]bool
	members   map[string]bool
	workspace string
}

func (a fakeAccess) HasWorkspacePermission(userID, workspaceID, resource, action string, isSystemAdmin bool) bool {
	return workspaceID == a.workspace && resource == "conversations" && action == "read" && (isSystemAdmin || a.readers[userID])
}

func (a fakeAccess) CanAccessEntry(userID, workspaceID, entryID, entryType string, isAdmin bool) bool {
	return workspaceID == a.workspace && entryID == chat.EntryID && entryType == string(chat.EntryType) && (isAdmin || a.seers[userID])
}

func (a fakeAccess) IsWorkspaceMember(userID, workspaceID string) bool {
	return workspaceID == a.workspace && a.members[userID]
}

type fakePeople map[string]string

func (p fakePeople) FindByIDs(ids []string) ([]*user.User, error) {
	var out []*user.User
	for _, id := range ids {
		if name, ok := p[id]; ok {
			out = append(out, &user.User{ID: id, Username: name})
		}
	}
	return out, nil
}

type recordingAnnouncer struct {
	created []conversationnote.Authored
	deleted []conversationnote.Note
}

func (a *recordingAnnouncer) AnnounceNote(note conversationnote.Authored) {
	a.created = append(a.created, note)
}

func (a *recordingAnnouncer) AnnounceNoteDeleted(note conversationnote.Note) {
	a.deleted = append(a.deleted, note)
}

type recordingPublisher struct {
	drafts []notification.Draft
	err    error
}

func (p *recordingPublisher) Publish(_ context.Context, d notification.Draft) (notification.Record, error) {
	p.drafts = append(p.drafts, d)
	if p.err != nil {
		return notification.Record{}, p.err
	}
	return notification.Record{}, nil
}

type fakeContacts map[shared.EntryRef]conversation.EntryInfo

func (c fakeContacts) GetEntryInfo(entryID, entryType string) (conversation.EntryInfo, error) {
	info, ok := c[shared.EntryRef{EntryID: entryID, EntryType: shared.EntryType(entryType)}]
	if !ok {
		return conversation.EntryInfo{}, errors.New("no such entry")
	}
	return info, nil
}

type memoryMedia struct {
	rows map[string]*conversation.ConversationMedia
	err  error
	asks [][]string
}

func newMemoryMedia() *memoryMedia {
	return &memoryMedia{rows: map[string]*conversation.ConversationMedia{}}
}

func (m *memoryMedia) ListByIDs(ids []string) ([]*conversation.ConversationMedia, error) {
	m.asks = append(m.asks, append([]string{}, ids...))
	if m.err != nil {
		return nil, m.err
	}
	var out []*conversation.ConversationMedia
	for _, id := range ids {
		if row, ok := m.rows[id]; ok {
			out = append(out, row)
		}
	}
	return out, nil
}

type recordingUploader struct {
	media    *memoryMedia
	received []conversation.UploadMediaInput
	err      error
}

func (u *recordingUploader) Execute(in conversation.UploadMediaInput) (*conversation.ConversationMedia, error) {
	u.received = append(u.received, in)
	if u.err != nil {
		return nil, u.err
	}
	id := "media-" + strings.Repeat("m", len(u.received))
	row := &conversation.ConversationMedia{
		ID: id, EntryID: in.EntryID, EntryType: shared.EntryType(in.EntryType), Type: in.MediaType,
		MimeType: in.MimeType, URL: "https://cdn/" + id, OriginalFilename: in.Filename, SizeBytes: int64(len(in.Data)),
		Layout: conversation.MediaLayout{Width: 640, Height: 480, Thumbhash: "hash-" + id},
	}
	u.media.rows[id] = row
	return row, nil
}
