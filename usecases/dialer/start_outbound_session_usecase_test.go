package dialer_usecase

import (
	"context"
	"errors"
	"testing"

	"vozko/domain/dialer"
)

type stubDialerRepo struct {
	active    *dialer.Session
	findErr   error
	createErr error
	created   *dialer.Session
}

func (r *stubDialerRepo) Create(session *dialer.Session) error {
	r.created = session
	return r.createErr
}

func (r *stubDialerRepo) Update(session *dialer.Session) error {
	return nil
}

func (r *stubDialerRepo) FindByID(string) (*dialer.Session, error) {
	return nil, dialer.ErrSessionNotFound
}

func (r *stubDialerRepo) FindActiveByOwnerConnection(string, string) (*dialer.Session, error) {
	if r.findErr != nil {
		return nil, r.findErr
	}
	if r.active != nil {
		return r.active, nil
	}
	return nil, dialer.ErrSessionNotFound
}

func (r *stubDialerRepo) ListActiveByWorkspace(string) ([]*dialer.Session, error) {
	return nil, nil
}

func TestStartOutboundSessionCreatesPendingSession(t *testing.T) {
	repo := &stubDialerRepo{}
	uc := NewStartOutboundSessionUseCase(repo)

	session, err := uc.Execute(context.Background(), dialer.StartOutboundSessionInput{
		WorkspaceID:       "ws-1",
		OwnerUserID:       "user-1",
		OwnerConnectionID: "conn-1",
		EntryID:           "entry-1",
		EntryType:         "whatsapp",
		TargetPhone:       "5511999999999",
	})
	if err != nil {
		t.Fatalf("Execute() error = %v", err)
	}
	if session == nil {
		t.Fatal("Execute() session = nil")
	}
	if session.Status != dialer.SessionStatusPending {
		t.Fatalf("Status = %q, want %q", session.Status, dialer.SessionStatusPending)
	}
	if repo.created == nil || repo.created.ID == "" {
		t.Fatal("expected repo.Create to receive a persisted session with an ID")
	}
}

func TestStartOutboundSessionRejectsActiveOwnerSession(t *testing.T) {
	repo := &stubDialerRepo{active: &dialer.Session{ID: "existing"}}
	uc := NewStartOutboundSessionUseCase(repo)

	_, err := uc.Execute(context.Background(), dialer.StartOutboundSessionInput{
		WorkspaceID:       "ws-1",
		OwnerUserID:       "user-1",
		OwnerConnectionID: "conn-1",
		TargetPhone:       "5511999999999",
	})
	if !errors.Is(err, dialer.ErrSessionAlreadyActive) {
		t.Fatalf("Execute() error = %v, want ErrSessionAlreadyActive", err)
	}
}

func TestStartOutboundSessionValidatesRequiredFields(t *testing.T) {
	repo := &stubDialerRepo{}
	uc := NewStartOutboundSessionUseCase(repo)

	_, err := uc.Execute(context.Background(), dialer.StartOutboundSessionInput{
		WorkspaceID:       "ws-1",
		OwnerUserID:       "user-1",
		OwnerConnectionID: "conn-1",
	})
	if !errors.Is(err, dialer.ErrTargetPhoneRequired) {
		t.Fatalf("Execute() error = %v, want ErrTargetPhoneRequired", err)
	}
}
