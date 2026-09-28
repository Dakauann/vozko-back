package facebook

import (
	"context"
	"errors"
	"testing"

	fbdomain "vozko/domain/facebook"
)

func connectedPage(id, workspaceID string) *fbdomain.Page {
	return &fbdomain.Page{
		ID: id, WorkspaceID: workspaceID, FBPageID: "fb-" + id, GrantID: "grant-1", PageToken: "pt",
		Status: fbdomain.StatusConnected, Tasks: []fbdomain.Task{fbdomain.TaskManage}, GrantedScopes: fbdomain.RequiredScopes(),
	}
}

func TestGetPageIsWorkspaceScoped(t *testing.T) {
	pages := newFakePages(connectedPage("p1", "ws-1"))
	uc := NewPageUseCases(pages, newFakeGrants(), &fakeSubscription{})
	if _, err := uc.Get(context.Background(), "ws-2", "p1"); !errors.Is(err, fbdomain.ErrPageNotFound) {
		t.Fatalf("cross-workspace read allowed: %v", err)
	}
	if p, err := uc.Get(context.Background(), "ws-1", "p1"); err != nil || p.ID != "p1" {
		t.Fatalf("got %+v, %v", p, err)
	}
}

func TestUpdateConfigWritesOnlyAutomationFields(t *testing.T) {
	pages := newFakePages(connectedPage("p1", "ws-1"))
	uc := NewPageUseCases(pages, newFakeGrants(), &fakeSubscription{})
	agent := "agent-1"
	updated, err := uc.UpdateConfig(context.Background(), UpdatePageConfigInput{
		WorkspaceID: "ws-1", ID: "p1", AgentID: &agent, EnableAgentResponses: true, AutomationDisclosure: "  Sou um assistente.  ",
	})
	if err != nil {
		t.Fatal(err)
	}
	if updated.AgentID == nil || !updated.EnableAgentResponses || updated.AutomationDisclosure != "Sou um assistente." || updated.PageToken != "pt" {
		t.Fatalf("updated = %+v", updated)
	}
}

func TestUpdateConfigRejectsForeignWorkspace(t *testing.T) {
	uc := NewPageUseCases(newFakePages(connectedPage("p1", "ws-1")), newFakeGrants(), &fakeSubscription{})
	if _, err := uc.UpdateConfig(context.Background(), UpdatePageConfigInput{WorkspaceID: "ws-2", ID: "p1"}); !errors.Is(err, fbdomain.ErrPageNotFound) {
		t.Fatalf("got %v", err)
	}
}

func TestDisconnectUnsubscribesMarksAndDeletes(t *testing.T) {
	pages := newFakePages(connectedPage("p1", "ws-1"))
	sub := &fakeSubscription{}
	grants := newFakeGrants()
	grants.byID["grant-1"] = &fbdomain.Grant{ID: "grant-1", Status: fbdomain.GrantActive}
	uc := NewPageUseCases(pages, grants, sub)

	warning, err := uc.Disconnect(context.Background(), "ws-1", "p1")
	if err != nil || warning != "" {
		t.Fatalf("warning=%q err=%v", warning, err)
	}
	if len(sub.unsubscribed) != 1 || pages.statuses["p1"] != fbdomain.StatusDisconnected || !pages.deleted["p1"] {
		t.Fatalf("unsub=%v status=%s deleted=%t", sub.unsubscribed, pages.statuses["p1"], pages.deleted["p1"])
	}
	if len(grants.revoked) != 1 || len(grants.erased) != 1 {
		t.Fatalf("a grant with no pages left must be revoked and its token erased: revoked=%v erased=%v", grants.revoked, grants.erased)
	}
}

func TestDisconnectStillCompletesWhenUnsubscribeFails(t *testing.T) {
	pages := newFakePages(connectedPage("p1", "ws-1"))
	uc := NewPageUseCases(pages, newFakeGrants(), &fakeSubscription{err: errors.New("token revoked")})
	warning, err := uc.Disconnect(context.Background(), "ws-1", "p1")
	if err != nil {
		t.Fatal(err)
	}
	if warning == "" || !pages.deleted["p1"] {
		t.Fatalf("warning=%q deleted=%t", warning, pages.deleted["p1"])
	}
}

func TestDisconnectKeepsAGrantThatStillServesPages(t *testing.T) {
	pages := newFakePages(connectedPage("p1", "ws-1"), connectedPage("p2", "ws-1"))
	grants := newFakeGrants()
	grants.byID["grant-1"] = &fbdomain.Grant{ID: "grant-1", Status: fbdomain.GrantActive}
	uc := NewPageUseCases(pages, grants, &fakeSubscription{})
	if _, err := uc.Disconnect(context.Background(), "ws-1", "p1"); err != nil {
		t.Fatal(err)
	}
	if len(grants.revoked) != 0 {
		t.Fatal("grant revoked while another page still uses it")
	}
}

type fakeFetcher struct {
	data        []byte
	contentType string
	err         error
}

func (f fakeFetcher) FetchBytes(context.Context, string) ([]byte, string, error) {
	return f.data, f.contentType, f.err
}

type fakeStorage struct {
	uploaded map[string][]byte
}

func (s *fakeStorage) UploadFile(key string, data []byte, _ string) error {
	s.uploaded[key] = data
	return nil
}
func (s *fakeStorage) GetFileURL(key string) string { return "https://cdn.vozko/" + key }

func TestPictureStoreReHostsTheImage(t *testing.T) {
	storage := &fakeStorage{uploaded: map[string][]byte{}}
	store := NewPictureStore(fakeFetcher{data: []byte("png"), contentType: "image/png"}, storage)
	key, err := store.StorePagePicture(context.Background(), "p1", "https://scontent.fbcdn/x.png?oe=1")
	if err != nil {
		t.Fatal(err)
	}
	if key != "facebook/pages/p1/picture.png" || string(storage.uploaded[key]) != "png" {
		t.Fatalf("key=%q uploaded=%v", key, storage.uploaded)
	}
	if store.URL(key) != "https://cdn.vozko/facebook/pages/p1/picture.png" || store.URL("") != "" {
		t.Fatal("url resolution wrong")
	}
}

func TestPictureStoreRejectsNonImages(t *testing.T) {
	store := NewPictureStore(fakeFetcher{data: []byte("<html>"), contentType: "text/html"}, &fakeStorage{uploaded: map[string][]byte{}})
	if _, err := store.StorePagePicture(context.Background(), "p1", "https://x"); err == nil {
		t.Fatal("non image stored")
	}
}
