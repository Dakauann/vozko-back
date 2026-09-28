package facebook

import (
	"context"
	"errors"
	"testing"

	fbdomain "vozko/domain/facebook"
	"vozko/infra/meta"
)

type fakeProfileService struct {
	current *fbdomain.MessengerProfile
	set     []fbdomain.MessengerProfile
	deleted [][]string
	setErr  error
}

func (f *fakeProfileService) Get(context.Context, string, string) (*fbdomain.MessengerProfile, error) {
	return f.current, nil
}
func (f *fakeProfileService) Set(_ context.Context, _, _ string, p fbdomain.MessengerProfile) error {
	f.set = append(f.set, p)
	return f.setErr
}
func (f *fakeProfileService) Delete(_ context.Context, _, _ string, fields []string) error {
	f.deleted = append(f.deleted, fields)
	return nil
}

func newProfileFixture() (*MessengerProfileUseCases, *fakeProfileService, *fakePages) {
	service, pages := &fakeProfileService{current: &fbdomain.MessengerProfile{}}, newFakePages(messagingPage())
	return NewMessengerProfileUseCases(pages, service), service, pages
}

func TestProfileUpdateSetsWhatIsPresentAndClearsTheRest(t *testing.T) {
	uc, service, _ := newProfileFixture()
	err := uc.Update(context.Background(), "ws-1", "page-1", fbdomain.MessengerProfile{
		Greeting: []fbdomain.LocalizedText{{Locale: "default", Text: "Olá"}},
	})
	if err != nil {
		t.Fatal(err)
	}
	if len(service.set) != 1 || len(service.deleted) != 1 || len(service.deleted[0]) != 3 {
		t.Fatalf("set %v deleted %v", service.set, service.deleted)
	}
}

func TestClearingEverythingOnlyDeletes(t *testing.T) {
	uc, service, _ := newProfileFixture()
	if err := uc.Update(context.Background(), "ws-1", "page-1", fbdomain.MessengerProfile{}); err != nil {
		t.Fatal(err)
	}
	if len(service.set) != 0 || len(service.deleted[0]) != 4 {
		t.Fatalf("set %v deleted %v", service.set, service.deleted)
	}
}

func TestAnInvalidProfileNeverReachesMeta(t *testing.T) {
	uc, service, _ := newProfileFixture()
	err := uc.Update(context.Background(), "ws-1", "page-1", fbdomain.MessengerProfile{
		PersistentMenu: []fbdomain.PersistentMenu{{Locale: "default", Items: []fbdomain.MenuItem{{Type: fbdomain.MenuPostback, Title: "x", Payload: "y"}}}},
	})
	if !errors.Is(err, fbdomain.ErrInvalidProfile) || len(service.set)+len(service.deleted) != 0 {
		t.Fatalf("err %v set %v", err, service.set)
	}
}

func TestProfileThrottleSurfacesAsRateLimited(t *testing.T) {
	uc, service, _ := newProfileFixture()
	service.setErr = &meta.Error{Code: meta.CodeMessagingRate, IsTransient: true}
	err := uc.Update(context.Background(), "ws-1", "page-1", fbdomain.MessengerProfile{GetStarted: &fbdomain.GetStarted{Payload: "GO"}})
	if !errors.Is(err, fbdomain.ErrProfileRateLimited) {
		t.Fatalf("got %v", err)
	}
}

func TestProfileNeedsMessagingOnThisWorkspacesPage(t *testing.T) {
	uc, _, pages := newProfileFixture()
	if _, err := uc.Get(context.Background(), "ws-2", "page-1"); !errors.Is(err, fbdomain.ErrPageNotFound) {
		t.Fatalf("other workspace: %v", err)
	}
	pages.byID["page-1"].Tasks = []fbdomain.Task{fbdomain.TaskAnalyze}
	if _, err := uc.Get(context.Background(), "ws-1", "page-1"); !errors.Is(err, fbdomain.ErrCapabilityDenied) {
		t.Fatalf("no messaging: %v", err)
	}
}
