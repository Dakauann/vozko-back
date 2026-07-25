package support_inbox_usecase

import (
	"strings"
	"testing"

	"vozko/domain/shared"
	si "vozko/domain/support_inbox"
)

type mockInboxRepo struct {
	inboxes map[string]*si.SupportInbox
}

func newMockInboxRepo() *mockInboxRepo {
	return &mockInboxRepo{inboxes: make(map[string]*si.SupportInbox)}
}

func (m *mockInboxRepo) Create(inbox *si.SupportInbox) error {
	m.inboxes[inbox.ID] = inbox
	return nil
}
func (m *mockInboxRepo) Update(id string, inbox *si.SupportInbox) error {
	m.inboxes[id] = inbox
	return nil
}
func (m *mockInboxRepo) Delete(id string) error {
	delete(m.inboxes, id)
	return nil
}
func (m *mockInboxRepo) FindByID(id string) (*si.SupportInbox, error) {
	if inbox, ok := m.inboxes[id]; ok {
		return inbox, nil
	}
	return nil, si.ErrInboxNotFound
}
func (m *mockInboxRepo) List(_ si.ListInboxesInput) (*shared.PaginatedResult[*si.SupportInbox], error) {
	items := make([]*si.SupportInbox, 0, len(m.inboxes))
	for _, v := range m.inboxes {
		items = append(items, v)
	}
	return &shared.PaginatedResult[*si.SupportInbox]{Items: items, TotalItems: int64(len(items))}, nil
}

type mockEntryRepo struct {
	entries map[string]*si.SupportEntry
}

func newMockEntryRepo() *mockEntryRepo {
	return &mockEntryRepo{entries: make(map[string]*si.SupportEntry)}
}
func (m *mockEntryRepo) Create(entry *si.SupportEntry) error {
	m.entries[entry.ID] = entry
	return nil
}
func (m *mockEntryRepo) FindByID(id string) (*si.SupportEntry, error) {
	if e, ok := m.entries[id]; ok {
		return e, nil
	}
	return nil, si.ErrEntryNotFound
}
func (m *mockEntryRepo) Delete(id string) error {
	delete(m.entries, id)
	return nil
}
func (m *mockEntryRepo) List(_ si.ListEntriesInput) (*shared.PaginatedResult[*si.SupportEntry], error) {
	return &shared.PaginatedResult[*si.SupportEntry]{}, nil
}
func (m *mockEntryRepo) CountByInboxID(_ string) (int64, error) {
	return int64(len(m.entries)), nil
}

type mockSessionRepo struct {
	sessions map[string]*si.SupportSession
}

func newMockSessionRepo() *mockSessionRepo {
	return &mockSessionRepo{sessions: make(map[string]*si.SupportSession)}
}
func (m *mockSessionRepo) Create(s *si.SupportSession) error {
	m.sessions[s.Token] = s
	return nil
}
func (m *mockSessionRepo) FindByToken(token string) (*si.SupportSession, error) {
	if s, ok := m.sessions[token]; ok {
		return s, nil
	}
	return nil, si.ErrSessionNotFound
}
func (m *mockSessionRepo) DeleteExpired() (int64, error) { return 0, nil }

func TestCreateSession_Success(t *testing.T) {
	inboxRepo := newMockInboxRepo()
	entryRepo := newMockEntryRepo()
	sessionRepo := newMockSessionRepo()

	inbox := &si.SupportInbox{
		ID:              "inbox-1",
		WorkspaceID:     "ws-1",
		Name:            "Support",
		GreetingMessage: "Hello, how can I help?",
		AllowedOrigins:  []string{"https://example.com"},
	}
	inboxRepo.inboxes[inbox.ID] = inbox

	uc := NewCreateSessionUseCase(inboxRepo, entryRepo, sessionRepo, "test-secret")

	output, err := uc.Execute(si.CreateSessionInput{
		InboxID:      "inbox-1",
		Origin:       "https://example.com",
		ContactName:  "John",
		ContactEmail: "john@test.com",
		SourceURL:    "https://example.com/help",
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if output.SessionToken == "" {
		t.Error("expected non-empty session token")
	}
	if output.EntryID == "" {
		t.Error("expected non-empty entry ID")
	}
	if output.GreetingMessage != "Hello, how can I help?" {
		t.Errorf("expected greeting message, got %q", output.GreetingMessage)
	}

	if len(entryRepo.entries) != 1 {
		t.Errorf("expected 1 entry, got %d", len(entryRepo.entries))
	}

	if len(sessionRepo.sessions) != 1 {
		t.Errorf("expected 1 session, got %d", len(sessionRepo.sessions))
	}
}

func TestCreateSession_CORSBlocked(t *testing.T) {
	inboxRepo := newMockInboxRepo()
	entryRepo := newMockEntryRepo()
	sessionRepo := newMockSessionRepo()

	inbox := &si.SupportInbox{
		ID:             "inbox-1",
		WorkspaceID:    "ws-1",
		Name:           "Support",
		AllowedOrigins: []string{"https://example.com"},
	}
	inboxRepo.inboxes[inbox.ID] = inbox

	uc := NewCreateSessionUseCase(inboxRepo, entryRepo, sessionRepo, "test-secret")

	_, err := uc.Execute(si.CreateSessionInput{
		InboxID: "inbox-1",
		Origin:  "https://malicious-site.com",
	})
	if err == nil {
		t.Fatal("expected CORS error")
	}
	if !strings.Contains(err.Error(), "not allowed") {
		t.Errorf("expected 'not allowed' in error, got: %v", err)
	}

	if len(entryRepo.entries) != 0 {
		t.Errorf("expected 0 entries after CORS block, got %d", len(entryRepo.entries))
	}
}

func TestCreateSession_WildcardOriginAllowsAll(t *testing.T) {
	inboxRepo := newMockInboxRepo()
	entryRepo := newMockEntryRepo()
	sessionRepo := newMockSessionRepo()

	inbox := &si.SupportInbox{
		ID:             "inbox-1",
		WorkspaceID:    "ws-1",
		Name:           "Support",
		AllowedOrigins: []string{"*"},
	}
	inboxRepo.inboxes[inbox.ID] = inbox

	uc := NewCreateSessionUseCase(inboxRepo, entryRepo, sessionRepo, "test-secret")

	_, err := uc.Execute(si.CreateSessionInput{
		InboxID: "inbox-1",
		Origin:  "https://any-site.com",
	})
	if err != nil {
		t.Fatalf("wildcard origin should allow any origin, got: %v", err)
	}
}

func TestCreateSession_NoOriginsAllowsAll(t *testing.T) {
	inboxRepo := newMockInboxRepo()
	entryRepo := newMockEntryRepo()
	sessionRepo := newMockSessionRepo()

	inbox := &si.SupportInbox{
		ID:             "inbox-1",
		WorkspaceID:    "ws-1",
		Name:           "Support",
		AllowedOrigins: nil,
	}
	inboxRepo.inboxes[inbox.ID] = inbox

	uc := NewCreateSessionUseCase(inboxRepo, entryRepo, sessionRepo, "test-secret")

	_, err := uc.Execute(si.CreateSessionInput{
		InboxID: "inbox-1",
		Origin:  "https://any-site.com",
	})
	if err != nil {
		t.Fatalf("nil origins should allow any origin, got: %v", err)
	}
}

func TestCreateSession_InboxNotFound(t *testing.T) {
	inboxRepo := newMockInboxRepo()
	entryRepo := newMockEntryRepo()
	sessionRepo := newMockSessionRepo()

	uc := NewCreateSessionUseCase(inboxRepo, entryRepo, sessionRepo, "test-secret")

	_, err := uc.Execute(si.CreateSessionInput{
		InboxID: "nonexistent",
		Origin:  "https://example.com",
	})
	if err != si.ErrInboxNotFound {
		t.Errorf("expected ErrInboxNotFound, got %v", err)
	}
}

func TestCreateInbox_Success(t *testing.T) {
	repo := newMockInboxRepo()
	uc := NewCreateInboxUseCase(repo)

	inbox := &si.SupportInbox{
		Name:           "Help Desk",
		WorkspaceID:    "ws-1",
		AllowedOrigins: []string{"https://example.com"},
	}

	result, err := uc.Execute(inbox)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if result.ID == "" {
		t.Error("expected generated ID")
	}
	if result.Archived {
		t.Error("new inbox should not be archived")
	}
	if result.CreatedAt.IsZero() {
		t.Error("expected CreatedAt to be set")
	}
}

func TestCreateInbox_ValidationError(t *testing.T) {
	repo := newMockInboxRepo()
	uc := NewCreateInboxUseCase(repo)

	inbox := &si.SupportInbox{
		Name:        "",
		WorkspaceID: "ws-1",
	}

	_, err := uc.Execute(inbox)
	if err == nil {
		t.Fatal("expected validation error for empty name")
	}
}
