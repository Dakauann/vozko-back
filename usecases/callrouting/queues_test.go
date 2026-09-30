package callrouting_usecase

import (
	"context"
	"errors"
	"testing"
	"time"

	"vozko/domain/callrouting"
	"vozko/domain/workspace"
	dept_domain "vozko/domain/workspace/workspace_department"
)

type storedQueues struct {
	queueBook
	created, updated []*callrouting.Queue
}

func (s *storedQueues) Create(_ context.Context, queue *callrouting.Queue) error {
	queue.ID = "q-new"
	s.created = append(s.created, queue)
	return nil
}

func (s *storedQueues) Update(_ context.Context, queue *callrouting.Queue) error {
	s.updated = append(s.updated, queue)
	return nil
}

type departmentBook map[string]*dept_domain.Department

func (d departmentBook) GetDepartmentByID(id string) (*dept_domain.Department, error) {
	department, ok := d[id]
	if !ok {
		return nil, dept_domain.ErrDepartmentNotFound
	}
	return department, nil
}

type memberBook map[string]bool

func (m memberBook) GetMember(workspaceID, userID string) (*workspace.Member, error) {
	if workspaceID != "ws1" || !m[userID] {
		return nil, nil
	}
	return &workspace.Member{WorkspaceID: workspaceID, UserID: userID}, nil
}

type presetsOnly struct{ callrouting.HoldMusicLibrary }

func (presetsOnly) PCM(_ context.Context, _ string, ref callrouting.HoldMusicRef) ([]byte, error) {
	if ref.PresetID == "piano_calmo" || ref.PresetID == "lofi" {
		return []byte{1}, nil
	}
	return nil, callrouting.ErrHoldMusicNotFound
}

func newCatalog(existing queueBook) (*QueueCatalog, *storedQueues) {
	queues := &storedQueues{queueBook: existing}
	catalog := NewQueueCatalog(QueueCatalogDeps{
		Queues:      queues,
		Departments: departmentBook{"vendas": {ID: "vendas", WorkspaceID: "ws1"}, "alheio": {ID: "alheio", WorkspaceID: "ws2"}},
		Members:     memberBook{"ana": true, "bia": true},
		Music:       presetsOnly{},
	})
	return catalog, queues
}

func TestANewQueueGetsTheIndustryDefaults(t *testing.T) {
	catalog, queues := newCatalog(nil)
	queue, err := catalog.Create(context.Background(), callrouting.Queue{WorkspaceID: "ws1", Name: " Vendas ", MemberUserIDs: []string{"ana", "bia", "ana"}})
	if err != nil {
		t.Fatalf("Create: %v", err)
	}
	if queue.ID != "q-new" || queue.Name != "Vendas" || queue.Strategy != callrouting.StrategyLongestIdle || queue.HoldMusic.PresetID != callrouting.DefaultHoldPreset || len(queue.MemberUserIDs) != 2 {
		t.Fatalf("queue = %+v", queue)
	}
	if len(queues.created) != 1 {
		t.Fatal("the queue was not stored")
	}
}

func TestQueuesOnlyAdmitWhatBelongsToTheWorkspace(t *testing.T) {
	cases := []struct {
		name  string
		queue callrouting.Queue
		want  error
	}{
		{"department from another workspace", callrouting.Queue{WorkspaceID: "ws1", Name: "X", DepartmentID: "alheio"}, callrouting.ErrQueueDepartment},
		{"unknown department", callrouting.Queue{WorkspaceID: "ws1", Name: "X", DepartmentID: "nope"}, callrouting.ErrQueueDepartment},
		{"stranger as member", callrouting.Queue{WorkspaceID: "ws1", Name: "X", MemberUserIDs: []string{"ana", "zé"}}, callrouting.ErrQueueMemberOutside},
		{"unknown hold music", callrouting.Queue{WorkspaceID: "ws1", Name: "X", MemberUserIDs: []string{"ana"}, HoldMusic: callrouting.HoldMusicRef{PresetID: "heavy_metal"}}, callrouting.ErrHoldMusicNotFound},
		{"no members", callrouting.Queue{WorkspaceID: "ws1", Name: "X"}, callrouting.ErrQueueMembersRequired},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			catalog, queues := newCatalog(nil)
			if _, err := catalog.Create(context.Background(), tc.queue); !errors.Is(err, tc.want) {
				t.Fatalf("err = %v, want %v", err, tc.want)
			}
			if len(queues.created) != 0 {
				t.Fatal("a refused queue was stored")
			}
		})
	}
}

func TestUpdatingAQueueKeepsItsBirthAndStaysInItsWorkspace(t *testing.T) {
	born := time.Date(2026, 9, 1, 0, 0, 0, 0, time.UTC)
	catalog, queues := newCatalog(queueBook{"q1": {ID: "q1", WorkspaceID: "ws1", Name: "Vendas", CreatedAt: born}})

	queue, err := catalog.Update(context.Background(), callrouting.Queue{ID: "q1", WorkspaceID: "ws1", Name: "Vendas N2", DepartmentID: "vendas", HoldMusic: callrouting.HoldMusicRef{PresetID: "lofi"}})
	if err != nil {
		t.Fatalf("Update: %v", err)
	}
	if !queue.CreatedAt.Equal(born) || queue.Name != "Vendas N2" || len(queues.updated) != 1 {
		t.Fatalf("queue = %+v", queue)
	}
	if _, err := catalog.Update(context.Background(), callrouting.Queue{ID: "q1", WorkspaceID: "ws2", Name: "X", MemberUserIDs: []string{"ana"}}); !errors.Is(err, callrouting.ErrQueueNotFound) {
		t.Fatalf("cross-workspace update err = %v", err)
	}
}

type settingsBook struct {
	saved []callrouting.Settings
	fail  error
}

func (s *settingsBook) Get(_ context.Context, workspaceID string) (callrouting.Settings, error) {
	if s.fail != nil {
		return callrouting.Settings{}, s.fail
	}
	if len(s.saved) == 0 {
		return callrouting.DefaultSettings(workspaceID), nil
	}
	return s.saved[len(s.saved)-1], nil
}

func (s *settingsBook) Save(_ context.Context, settings callrouting.Settings) error {
	s.saved = append(s.saved, settings)
	return nil
}

func TestWorkspaceHoldMusicIsCheckedBeforeItIsSaved(t *testing.T) {
	book := &settingsBook{}
	settings := NewRoutingSettings(book, presetsOnly{})
	ctx := context.Background()

	if _, err := settings.Save(ctx, callrouting.Settings{WorkspaceID: "ws1", HoldMusic: callrouting.HoldMusicRef{PresetID: "heavy_metal"}}); !errors.Is(err, callrouting.ErrHoldMusicNotFound) {
		t.Fatalf("unknown preset err = %v", err)
	}
	if _, err := settings.Save(ctx, callrouting.Settings{HoldMusic: callrouting.HoldMusicRef{PresetID: "lofi"}}); !errors.Is(err, callrouting.ErrWorkspaceRequired) {
		t.Fatalf("no workspace err = %v", err)
	}
	if len(book.saved) != 0 {
		t.Fatal("refused settings were stored")
	}
	if _, err := settings.Save(ctx, callrouting.Settings{WorkspaceID: "ws1", HoldMusic: callrouting.HoldMusicRef{PresetID: "lofi"}}); err != nil {
		t.Fatalf("Save: %v", err)
	}
	if got := settings.HoldMusicFor(ctx, "ws1"); got.PresetID != "lofi" {
		t.Fatalf("HoldMusicFor = %+v", got)
	}
}

func TestHoldMusicFallsBackToTheDefaultPresetWhenSettingsAreUnreadable(t *testing.T) {
	settings := NewRoutingSettings(&settingsBook{fail: errors.New("db down")}, presetsOnly{})
	if got := settings.HoldMusicFor(context.Background(), "ws1"); got.PresetID != callrouting.DefaultHoldPreset {
		t.Fatalf("HoldMusicFor = %+v", got)
	}
}
