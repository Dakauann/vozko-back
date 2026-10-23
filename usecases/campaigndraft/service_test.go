package campaigndraft_usecase

import (
	"context"
	"encoding/json"
	"errors"
	"sort"
	"testing"
	"time"

	"vozko/domain/campaigndraft"
)

type memoryRepo struct {
	rows map[string]campaigndraft.Draft
}

func newMemoryRepo() *memoryRepo {
	return &memoryRepo{rows: map[string]campaigndraft.Draft{}}
}

func (m *memoryRepo) Create(_ context.Context, d *campaigndraft.Draft) error {
	m.rows[d.ID] = *d
	return nil
}

func (m *memoryRepo) Update(_ context.Context, d *campaigndraft.Draft) error {
	stored, ok := m.rows[d.ID]
	if !ok || stored.WorkspaceID != d.WorkspaceID || stored.Channel != d.Channel {
		return campaigndraft.ErrNotFound
	}
	m.rows[d.ID] = *d
	return nil
}

func (m *memoryRepo) Find(_ context.Context, workspaceID string, ch campaigndraft.Channel, id string) (*campaigndraft.Draft, error) {
	d, ok := m.rows[id]
	if !ok || d.WorkspaceID != workspaceID || d.Channel != ch {
		return nil, campaigndraft.ErrNotFound
	}
	return &d, nil
}

func (m *memoryRepo) List(_ context.Context, workspaceID string, ch campaigndraft.Channel, limit int) ([]campaigndraft.Draft, error) {
	var out []campaigndraft.Draft
	for _, d := range m.rows {
		if d.WorkspaceID == workspaceID && d.Channel == ch {
			out = append(out, d)
		}
	}
	sort.Slice(out, func(i, j int) bool { return out[i].UpdatedAt.After(out[j].UpdatedAt) })
	if len(out) > limit {
		out = out[:limit]
	}
	return out, nil
}

func (m *memoryRepo) Delete(_ context.Context, workspaceID string, ch campaigndraft.Channel, id string) error {
	if _, err := m.Find(context.Background(), workspaceID, ch, id); err != nil {
		return err
	}
	delete(m.rows, id)
	return nil
}

func service(t *testing.T) (*Service, *memoryRepo) {
	t.Helper()
	repo := newMemoryRepo()
	s, err := New(repo)
	if err != nil {
		t.Fatal(err)
	}
	clock := time.Date(2026, 10, 22, 12, 0, 0, 0, time.UTC)
	s.now = func() time.Time {
		clock = clock.Add(time.Second)
		return clock
	}
	seq := 0
	s.newID = func() string {
		seq++
		return []string{"", "d1", "d2", "d3", "d4"}[seq]
	}
	return s, repo
}

var me = Scope{WorkspaceID: "w1", UserID: "u1"}

func TestCreateSavesWhatIsFilledIn(t *testing.T) {
	s, repo := service(t)
	d, err := s.Create(context.Background(), me, campaigndraft.ChannelWhatsApp, "Natal", json.RawMessage(`{"request":{"name":"Natal"}}`))
	if err != nil {
		t.Fatal(err)
	}
	if d.ID != "d1" || d.CreatedBy != "u1" || repo.rows["d1"].Name != "Natal" {
		t.Fatalf("draft %+v", d)
	}
}

func TestCreateNeedsAPerson(t *testing.T) {
	s, _ := service(t)
	if _, err := s.Create(context.Background(), Scope{WorkspaceID: "w1"}, campaigndraft.ChannelWhatsApp, "", nil); !errors.Is(err, campaigndraft.ErrInvalid) {
		t.Fatalf("got %v", err)
	}
}

func TestUpdateRevisesAndKeepsTheAuthor(t *testing.T) {
	s, _ := service(t)
	created, _ := s.Create(context.Background(), me, campaigndraft.ChannelWhatsApp, "a", nil)
	updated, err := s.Update(context.Background(), Scope{WorkspaceID: "w1", UserID: "u2"}, campaigndraft.ChannelWhatsApp, created.ID, "b", json.RawMessage(`{"state":{"step":"audience"}}`))
	if err != nil {
		t.Fatal(err)
	}
	if updated.Name != "b" || updated.CreatedBy != "u1" || !updated.UpdatedAt.After(created.UpdatedAt) {
		t.Fatalf("updated %+v", updated)
	}
}

func TestDraftsAreScopedToWorkspaceAndChannel(t *testing.T) {
	s, _ := service(t)
	ctx := context.Background()
	created, _ := s.Create(ctx, me, campaigndraft.ChannelWhatsApp, "a", nil)
	other := Scope{WorkspaceID: "w2", UserID: "u9"}
	if _, err := s.Get(ctx, other, campaigndraft.ChannelWhatsApp, created.ID); !errors.Is(err, campaigndraft.ErrNotFound) {
		t.Fatalf("other workspace: %v", err)
	}
	if _, err := s.Get(ctx, me, campaigndraft.ChannelUnofficialWhatsApp, created.ID); !errors.Is(err, campaigndraft.ErrNotFound) {
		t.Fatalf("other channel: %v", err)
	}
	if _, err := s.Update(ctx, other, campaigndraft.ChannelWhatsApp, created.ID, "x", nil); !errors.Is(err, campaigndraft.ErrNotFound) {
		t.Fatalf("other workspace update: %v", err)
	}
	if err := s.Delete(ctx, other, campaigndraft.ChannelWhatsApp, created.ID); !errors.Is(err, campaigndraft.ErrNotFound) {
		t.Fatalf("other workspace delete: %v", err)
	}
	listed, err := s.List(ctx, other, campaigndraft.ChannelWhatsApp)
	if err != nil || len(listed) != 0 {
		t.Fatalf("other workspace listed %v %v", listed, err)
	}
}

func TestListNewestFirstAndDelete(t *testing.T) {
	s, _ := service(t)
	ctx := context.Background()
	first, _ := s.Create(ctx, me, campaigndraft.ChannelWhatsApp, "first", nil)
	second, _ := s.Create(ctx, me, campaigndraft.ChannelWhatsApp, "second", nil)
	listed, err := s.List(ctx, me, campaigndraft.ChannelWhatsApp)
	if err != nil || len(listed) != 2 || listed[0].ID != second.ID {
		t.Fatalf("listed %v %v", listed, err)
	}
	if err := s.Delete(ctx, me, campaigndraft.ChannelWhatsApp, first.ID); err != nil {
		t.Fatal(err)
	}
	if _, err := s.Get(ctx, me, campaigndraft.ChannelWhatsApp, first.ID); !errors.Is(err, campaigndraft.ErrNotFound) {
		t.Fatalf("got %v", err)
	}
}

func TestListRefusesAnUnknownChannel(t *testing.T) {
	s, _ := service(t)
	if _, err := s.List(context.Background(), me, campaigndraft.Channel("sms")); !errors.Is(err, campaigndraft.ErrInvalid) {
		t.Fatalf("got %v", err)
	}
}
