package metaplatform

import (
	"context"
	"errors"
	"testing"
	"time"

	mp "vozko/domain/metaplatform"
)

type memoryRequests struct {
	rows map[string]*mp.DeletionRequest
}

func (m *memoryRequests) Create(_ context.Context, r *mp.DeletionRequest) error {
	stored := *r
	m.rows[r.Code] = &stored
	return nil
}

func (m *memoryRequests) FindByCode(_ context.Context, code string) (*mp.DeletionRequest, error) {
	r, ok := m.rows[code]
	if !ok {
		return nil, mp.ErrRequestNotFound
	}
	return r, nil
}

func (m *memoryRequests) Finish(_ context.Context, code string, status mp.DeletionStatus, detail string, at time.Time) error {
	r := m.rows[code]
	r.Status, r.Detail, r.CompletedAt = status, detail, &at
	return nil
}

type recordingHandler struct {
	revoked, erased []string
	err             error
}

func (h *recordingHandler) RevokeAppUser(_ context.Context, id string) error {
	h.revoked = append(h.revoked, id)
	return h.err
}

func (h *recordingHandler) EraseAppUser(_ context.Context, id string) error {
	h.erased = append(h.erased, id)
	return h.err
}

func newService() (*Service, *memoryRequests) {
	repo := &memoryRequests{rows: map[string]*mp.DeletionRequest{}}
	return NewService(repo, "https://app.example"), repo
}

func TestDeauthorizeCallsOnlyTheAppsHandlers(t *testing.T) {
	svc, _ := newService()
	metaHandler, igHandler := &recordingHandler{}, &recordingHandler{}
	svc.Register(mp.AppMeta, metaHandler)
	svc.Register(mp.AppInstagram, igHandler)

	if err := svc.Deauthorize(context.Background(), mp.AppMeta, "asid-1"); err != nil {
		t.Fatal(err)
	}
	if len(metaHandler.revoked) != 1 || len(igHandler.revoked) != 0 {
		t.Fatalf("meta=%v instagram=%v", metaHandler.revoked, igHandler.revoked)
	}
}

func TestDeauthorizeRejectsUnknownAppAndBlankUser(t *testing.T) {
	svc, _ := newService()
	if err := svc.Deauthorize(context.Background(), "tiktok", "asid"); !errors.Is(err, mp.ErrUnknownApp) {
		t.Fatalf("got %v", err)
	}
	if err := svc.Deauthorize(context.Background(), mp.AppMeta, " "); !errors.Is(err, mp.ErrAppScopedUserIDRequired) {
		t.Fatalf("got %v", err)
	}
}

func TestDeletionRequestIsRecordedAndCompleted(t *testing.T) {
	svc, repo := newService()
	h := &recordingHandler{}
	svc.Register(mp.AppInstagram, h)

	out, err := svc.RequestDeletion(context.Background(), mp.AppInstagram, "asid-9")
	if err != nil {
		t.Fatal(err)
	}
	if len(out.Code) < 16 || out.StatusURL != "https://app.example/data-deletion?code="+out.Code {
		t.Fatalf("output = %+v", out)
	}
	row := repo.rows[out.Code]
	if row.Status != mp.DeletionCompleted || row.CompletedAt == nil || len(h.erased) != 1 {
		t.Fatalf("row = %+v erased=%v", row, h.erased)
	}
}

func TestDeletionFailureIsRecordedAsFailed(t *testing.T) {
	svc, repo := newService()
	svc.Register(mp.AppMeta, &recordingHandler{err: errors.New("db down")})

	out, err := svc.RequestDeletion(context.Background(), mp.AppMeta, "asid-9")
	if err != nil {
		t.Fatalf("the callback must still answer with a code: %v", err)
	}
	if repo.rows[out.Code].Status != mp.DeletionFailed {
		t.Fatalf("status = %s", repo.rows[out.Code].Status)
	}
}

func TestDeletionCodesAreUnique(t *testing.T) {
	svc, _ := newService()
	a, _ := svc.RequestDeletion(context.Background(), mp.AppMeta, "x")
	b, _ := svc.RequestDeletion(context.Background(), mp.AppMeta, "x")
	if a.Code == b.Code {
		t.Fatal("codes collide")
	}
}

func TestStatusLookup(t *testing.T) {
	svc, _ := newService()
	out, _ := svc.RequestDeletion(context.Background(), mp.AppMeta, "x")
	got, err := svc.Status(context.Background(), out.Code)
	if err != nil || got.Status != mp.DeletionCompleted {
		t.Fatalf("got %+v, %v", got, err)
	}
	if _, err := svc.Status(context.Background(), "missing"); !errors.Is(err, mp.ErrRequestNotFound) {
		t.Fatalf("got %v", err)
	}
}
