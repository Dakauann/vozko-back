package facebook

import (
	"context"
	"errors"
	"testing"

	fbdomain "vozko/domain/facebook"
	"vozko/infra/meta"
)

type scriptedOAuth struct {
	fakeOAuth
	debugByToken map[string]*fbdomain.TokenDebug
	debugErr     map[string]error
	pagesByToken map[string][]*fbdomain.RemotePage
}

func (s *scriptedOAuth) DebugToken(_ context.Context, token string) (*fbdomain.TokenDebug, error) {
	if err := s.debugErr[token]; err != nil {
		return nil, err
	}
	return s.debugByToken[token], nil
}

func (s *scriptedOAuth) ListPages(_ context.Context, token string) ([]*fbdomain.RemotePage, error) {
	return s.pagesByToken[token], nil
}

func healthFixture() (*HealthCheckUseCase, *scriptedOAuth, *fakeGrants, *fakePages, *fakeSubscription) {
	grants := newFakeGrants()
	grants.byID["g-ok"] = &fbdomain.Grant{ID: "g-ok", AccessToken: "tok-ok", Status: fbdomain.GrantActive}
	grants.byID["g-dead"] = &fbdomain.Grant{ID: "g-dead", AccessToken: "tok-dead", Status: fbdomain.GrantActive}

	okPage := connectedPage("p-ok", "ws-1")
	okPage.GrantID, okPage.FBPageID = "g-ok", "111"
	gonePage := connectedPage("p-gone", "ws-1")
	gonePage.GrantID, gonePage.FBPageID = "g-ok", "222"
	deadPage := connectedPage("p-dead", "ws-2")
	deadPage.GrantID, deadPage.FBPageID = "g-dead", "333"
	pages := newFakePages(okPage, gonePage, deadPage)

	oauth := &scriptedOAuth{
		debugByToken: map[string]*fbdomain.TokenDebug{"tok-ok": fullGrantDebug("111")},
		debugErr:     map[string]error{"tok-dead": &meta.Error{Code: meta.CodeAccessTokenError, Subcode: 463}},
		pagesByToken: map[string][]*fbdomain.RemotePage{"tok-ok": {{FBPageID: "111", Name: "Loja", AccessToken: "pt-new", Tasks: []fbdomain.Task{fbdomain.TaskManage}}}},
	}
	sub := &fakeSubscription{active: []string{"messages"}}
	uc := NewHealthCheckUseCase(oauth, sub, grants, pages, nil)
	return uc, oauth, grants, pages, sub
}

func TestHealthCheckRefreshesTokensAndResubscribesMissingFields(t *testing.T) {
	uc, _, grants, pages, sub := healthFixture()
	if err := uc.Execute(context.Background()); err != nil {
		t.Fatal(err)
	}
	ok := pages.byID["p-ok"]
	if ok.PageToken != "pt-new" || ok.Status != fbdomain.StatusConnected {
		t.Fatalf("healthy page = %+v", ok)
	}
	if len(sub.subscribed) != 1 || sub.subscribed[0] != "111" {
		t.Fatalf("missing fields must be re-subscribed: %v", sub.subscribed)
	}
	if len(grants.checked) != 1 || grants.checked[0] != "g-ok" {
		t.Fatalf("checked = %v", grants.checked)
	}
}

func TestHealthCheckMarksPagesThatLostAccess(t *testing.T) {
	uc, _, _, pages, _ := healthFixture()
	if err := uc.Execute(context.Background()); err != nil {
		t.Fatal(err)
	}
	if pages.statuses["p-gone"] != fbdomain.StatusTokenRevoked {
		t.Fatalf("page no longer granted = %s", pages.statuses["p-gone"])
	}
}

func TestHealthCheckRevokesADeadGrantWithoutStoppingTheLoop(t *testing.T) {
	uc, _, grants, pages, _ := healthFixture()
	if err := uc.Execute(context.Background()); err != nil {
		t.Fatal(err)
	}
	if len(grants.revoked) != 1 || grants.revoked[0] != "g-dead" {
		t.Fatalf("revoked = %v", grants.revoked)
	}
	if pages.statuses["p-dead"] != fbdomain.StatusTokenRevoked {
		t.Fatalf("page of a dead grant = %s", pages.statuses["p-dead"])
	}
	if pages.byID["p-ok"].PageToken != "pt-new" {
		t.Fatal("a failing tenant stopped the loop")
	}
}

func TestHealthCheckKeepsGrantsOnTransientErrors(t *testing.T) {
	uc, oauth, grants, _, _ := healthFixture()
	oauth.debugErr["tok-dead"] = errors.New("connection reset")
	if err := uc.Execute(context.Background()); err != nil {
		t.Fatal(err)
	}
	if len(grants.revoked) != 0 {
		t.Fatal("a transient failure revoked a grant")
	}
}

func TestAppUserHandlerRevokesEveryGrantOfTheUser(t *testing.T) {
	grants := newFakeGrants()
	grants.byID["g1"] = &fbdomain.Grant{ID: "g1", AppScopedUserID: "asid-9", Status: fbdomain.GrantActive}
	page := connectedPage("p1", "ws-1")
	page.GrantID = "g1"
	pages := newFakePages(page)
	h := NewAppUserHandler(grants, pages)

	if err := h.RevokeAppUser(context.Background(), "asid-9"); err != nil {
		t.Fatal(err)
	}
	if len(grants.revoked) != 1 || pages.statuses["p1"] != fbdomain.StatusTokenRevoked {
		t.Fatalf("revoked=%v status=%s", grants.revoked, pages.statuses["p1"])
	}
	if err := h.RevokeAppUser(context.Background(), "unknown"); err != nil {
		t.Fatalf("unknown user must be a no-op: %v", err)
	}
}

func TestAppUserHandlerEraseDeletesPagesAndTokens(t *testing.T) {
	grants := newFakeGrants()
	grants.byID["g1"] = &fbdomain.Grant{ID: "g1", AppScopedUserID: "asid-9", Status: fbdomain.GrantActive}
	page := connectedPage("p1", "ws-1")
	page.GrantID = "g1"
	pages := newFakePages(page)
	if err := NewAppUserHandler(grants, pages).EraseAppUser(context.Background(), "asid-9"); err != nil {
		t.Fatal(err)
	}
	if len(grants.erased) != 1 || !pages.deleted["p1"] || pages.byID["p1"].PageToken != "" {
		t.Fatalf("erased=%v deleted=%t token=%q", grants.erased, pages.deleted["p1"], pages.byID["p1"].PageToken)
	}
}

func TestCheckPageIsWorkspaceScoped(t *testing.T) {
	uc, _, _, _, _ := healthFixture()
	if _, err := uc.CheckPage(context.Background(), "ws-other", "p-ok"); !errors.Is(err, fbdomain.ErrPageNotFound) {
		t.Fatalf("got %v", err)
	}
	page, err := uc.CheckPage(context.Background(), "ws-1", "p-ok")
	if err != nil || page.PageToken != "pt-new" {
		t.Fatalf("got %+v, %v", page, err)
	}
}

func TestHealthCheckKeepsPagesOfAnUnrestrictedGrant(t *testing.T) {
	uc, oauth, _, pages, _ := healthFixture()
	oauth.debugByToken["tok-ok"] = fullGrantDebug()
	if err := uc.Execute(context.Background()); err != nil {
		t.Fatal(err)
	}
	if ok := pages.byID["p-ok"]; ok.Status != fbdomain.StatusConnected || ok.PageToken != "pt-new" {
		t.Fatalf("page = %+v", ok)
	}
	if pages.statuses["p-gone"] != fbdomain.StatusTokenRevoked {
		t.Fatal("a page the token no longer lists must still be revoked")
	}
}
