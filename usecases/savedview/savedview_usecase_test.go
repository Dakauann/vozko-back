package savedview_usecase

import (
	"errors"
	"sort"
	"strings"
	"testing"

	"vozko/domain/savedview"
)

type memoryViews struct {
	views   map[string]*savedview.SavedView
	updated int
	deleted int
}

func (m *memoryViews) Create(v *savedview.SavedView) error { m.views[v.ID] = v; return nil }
func (m *memoryViews) Update(v *savedview.SavedView) error {
	m.updated++
	m.views[v.ID] = v
	return nil
}
func (m *memoryViews) Delete(_, id string) error { m.deleted++; delete(m.views, id); return nil }
func (m *memoryViews) GetByID(_, id string) (*savedview.SavedView, error) {
	if v, ok := m.views[id]; ok {
		copied := *v
		return &copied, nil
	}
	return nil, savedview.ErrNotFound
}
func (m *memoryViews) ListForUser(string, string, savedview.ObjectType) ([]*savedview.SavedView, error) {
	out := make([]*savedview.SavedView, 0, len(m.views))
	for _, v := range m.views {
		out = append(out, v)
	}
	return out, nil
}
func (m *memoryViews) ClearDefault(string, string, savedview.ObjectType) error { return nil }

func sharedView() *memoryViews {
	return &memoryViews{views: map[string]*savedview.SavedView{
		"v1": {ID: "v1", WorkspaceID: "ws", OwnerID: "owner", ObjectType: savedview.ObjectLead, Name: "Bairro Centro",
			GroupBy: savedview.GroupByNone, Visibility: savedview.VisibilityShared, SortDir: savedview.SortDesc},
	}}
}

func TestSavedViewWrites_OnlyTheOwnerEdits(t *testing.T) {
	writes := []struct {
		name string
		run  func(repo *memoryViews, actor string) error
	}{
		{"update", func(repo *memoryViews, actor string) error {
			_, err := NewUpdateSavedViewUseCase(repo, viewerAccess()).Execute(savedviewActor(actor), "v1", &savedview.SavedView{Name: "Novo", ObjectType: savedview.ObjectLead})
			return err
		}},
		{"delete", func(repo *memoryViews, actor string) error {
			return NewDeleteSavedViewUseCase(repo, viewerAccess()).Execute(savedviewActor(actor), "v1")
		}},
		{"set default", func(repo *memoryViews, actor string) error {
			_, err := NewSetDefaultSavedViewUseCase(repo, viewerAccess()).Execute(savedviewActor(actor), "v1")
			return err
		}},
	}
	actors := []struct {
		name    string
		actor   string
		allowed bool
	}{
		{"owner", "owner", true},
		{"member reading a shared view", "member", false},
	}
	for _, w := range writes {
		for _, a := range actors {
			t.Run(w.name+" by "+a.name, func(t *testing.T) {
				repo := sharedView()
				err := w.run(repo, a.actor)
				if a.allowed {
					if err != nil {
						t.Fatalf("owner refused: %v", err)
					}
					return
				}
				if !errors.Is(err, savedview.ErrUnauthorized) || repo.updated+repo.deleted != 0 {
					t.Fatalf("want ErrUnauthorized with nothing written, got %v (%d writes)", err, repo.updated+repo.deleted)
				}
			})
		}
	}
}

func TestListSavedViews_ReturnsOnlyWhatTheViewerMayRead(t *testing.T) {
	repo := &memoryViews{views: map[string]*savedview.SavedView{
		"shared":         {ID: "shared", OwnerID: "owner", Visibility: savedview.VisibilityShared},
		"mine":           {ID: "mine", OwnerID: "member", Visibility: savedview.VisibilityPrivate},
		"someone else's": {ID: "someone else's", OwnerID: "owner", Visibility: savedview.VisibilityPrivate},
	}}
	cases := []struct {
		viewer string
		want   []string
	}{
		{"member", []string{"mine", "shared"}},
		{"owner", []string{"shared", "someone else's"}},
	}
	for _, tc := range cases {
		t.Run("viewer "+tc.viewer, func(t *testing.T) {
			views, err := NewListSavedViewsUseCase(repo, viewerAccess()).Execute(savedviewActor(tc.viewer), savedview.ObjectLead)
			if err != nil {
				t.Fatal(err)
			}
			got := make([]string, 0, len(views))
			for _, v := range views {
				got = append(got, v.ID)
			}
			sort.Strings(got)
			if strings.Join(got, ",") != strings.Join(tc.want, ",") {
				t.Fatalf("got %v, want %v", got, tc.want)
			}
		})
	}
}

func viewerAccess() Access {
	return access(grants{"leads:read": true}, &leadFilterCheck{})
}

func savedviewActor(userID string) savedview.Actor {
	return savedview.Actor{UserID: userID, WorkspaceID: "ws"}
}
