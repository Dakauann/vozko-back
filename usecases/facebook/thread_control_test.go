package facebook

import (
	"context"
	"errors"
	"testing"

	fbdomain "vozko/domain/facebook"
)

type fakeEntryAccess struct{ allowed bool }

func (f fakeEntryAccess) CanAccessEntry(_, _, _, _ string, _ bool) bool { return f.allowed }

type threadFixture struct {
	uc       *ThreadControlUseCase
	pages    *fakePages
	convs    *fakeConversations
	routing  *fakeRouting
	contacts *fakeContacts
	convID   string
}

var operator = ThreadActor{WorkspaceID: "ws-1", UserID: "user-1"}

func newThreadFixture(t *testing.T, access bool) *threadFixture {
	t.Helper()
	f := &threadFixture{pages: newFakePages(messagingPage()), contacts: newFakeContacts(), convs: newFakeConversations(), routing: &fakeRouting{}}
	contact, _ := f.contacts.FindOrCreate(context.Background(), "ws-1", "page-1", "psid-1")
	conv, _ := f.convs.FindOrCreate(context.Background(), "ws-1", "page-1", contact.ID)
	conv.ThreadOwnerAppID = fbdomain.PageInboxAppID
	f.convID = conv.ID
	f.uc = NewThreadControlUseCase(ThreadControlDeps{
		Pages: f.pages, Contacts: f.contacts, Conversations: f.convs, Routing: f.routing,
		Access: fakeEntryAccess{allowed: access}, OurAppID: ourAppID,
	})
	return f
}

func TestTakingControlClaimsTheThreadForUs(t *testing.T) {
	f := newThreadFixture(t, true)

	owner, err := f.uc.Take(context.Background(), operator, f.convID)
	if err != nil {
		t.Fatal(err)
	}
	if owner != ourAppID || len(f.routing.taken) != 1 || f.routing.taken[0] != "psid-1|vozko:operator" {
		t.Fatalf("owner %q, taken %v", owner, f.routing.taken)
	}
	if got := f.convs.byID[f.convID].ThreadOwnerAppID; got != "" {
		t.Fatalf("stored owner = %q", got)
	}
}

func TestReleasingControlSilencesAutomationUntilTheThreadReturns(t *testing.T) {
	f := newThreadFixture(t, true)
	f.convs.byID[f.convID].ThreadOwnerAppID = ""

	if err := f.uc.Release(context.Background(), operator, f.convID); err != nil {
		t.Fatal(err)
	}
	if len(f.routing.released) != 1 || f.convs.byID[f.convID].ThreadOwnerAppID != fbdomain.OtherAppOwner {
		t.Fatalf("released %v, owner %q", f.routing.released, f.convs.byID[f.convID].ThreadOwnerAppID)
	}
}

func TestThreadControlStaysInsideTheWorkspace(t *testing.T) {
	f := newThreadFixture(t, true)

	if _, err := f.uc.Take(context.Background(), ThreadActor{WorkspaceID: "ws-other", UserID: "user-1"}, f.convID); !errors.Is(err, fbdomain.ErrConversationNotFound) {
		t.Fatalf("other workspace: %v", err)
	}
	if len(f.routing.taken) != 0 {
		t.Fatal("control taken across workspaces")
	}
}

func TestThreadControlNeedsAccessToTheConversation(t *testing.T) {
	f := newThreadFixture(t, false)

	if _, err := f.uc.Take(context.Background(), operator, f.convID); !errors.Is(err, ErrConversationAccessDenied) {
		t.Fatalf("got %v", err)
	}
	if err := f.uc.Release(context.Background(), operator, f.convID); !errors.Is(err, ErrConversationAccessDenied) {
		t.Fatalf("got %v", err)
	}
	if len(f.routing.taken)+len(f.routing.released) != 0 {
		t.Fatal("thread control ran without access")
	}
}

func TestThreadControlNeedsMessaging(t *testing.T) {
	f := newThreadFixture(t, true)
	f.pages.byID["page-1"].Tasks = []fbdomain.Task{fbdomain.TaskAnalyze}

	if _, err := f.uc.Take(context.Background(), operator, f.convID); !errors.Is(err, fbdomain.ErrCapabilityDenied) {
		t.Fatalf("got %v", err)
	}
}

func TestRoutingProbeRecordsWhoOwnsTheLatestThread(t *testing.T) {
	cases := []struct {
		name  string
		owner string
		want  *bool
	}{
		{"us", ourAppID, boolPtr(true)},
		{"business suite", fbdomain.PageInboxAppID, boolPtr(false)},
		{"idle", "", nil},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			f := newThreadFixture(t, true)
			f.routing.owner = tc.owner

			f.uc.Probe(context.Background(), messagingPage())

			stored := f.pages.byID["page-1"]
			if stored.RoutingCheckedAt == nil {
				t.Fatal("probe recorded nothing")
			}
			got := stored.IsDefaultRouteApp
			if (got == nil) != (tc.want == nil) || (got != nil && *got != *tc.want) {
				t.Fatalf("recorded %v, want %v", got, tc.want)
			}
		})
	}
}

func TestRoutingProbeWithoutConversationsIsUnknown(t *testing.T) {
	pages := newFakePages(messagingPage())
	uc := NewThreadControlUseCase(ThreadControlDeps{
		Pages: pages, Contacts: newFakeContacts(), Conversations: newFakeConversations(),
		Routing: &fakeRouting{owner: ourAppID}, OurAppID: ourAppID,
	})

	uc.Probe(context.Background(), messagingPage())

	if stored := pages.byID["page-1"]; stored.RoutingCheckedAt == nil || stored.IsDefaultRouteApp != nil {
		t.Fatalf("recorded %v, want unknown", stored.IsDefaultRouteApp)
	}
}

func boolPtr(b bool) *bool { return &b }

func TestThreadStateNamesWhoHoldsTheThread(t *testing.T) {
	cases := map[string]ThreadHolder{
		"":                      HolderVozko,
		fbdomain.PageInboxAppID: HolderBusinessSuite,
		fbdomain.OtherAppOwner:  HolderOtherApp,
		"555":                   HolderOtherApp,
	}
	for owner, want := range cases {
		f := newThreadFixture(t, true)
		f.convs.byID[f.convID].ThreadOwnerAppID = owner
		yes := true
		f.pages.byID["page-1"].IsDefaultRouteApp = &yes

		state, err := f.uc.State(context.Background(), operator, f.convID)
		if err != nil || state.Holder != want || state.IsDefaultRouteApp == nil || !*state.IsDefaultRouteApp {
			t.Errorf("owner %q: state %+v, %v", owner, state, err)
		}
	}
}

func TestThreadStateNeedsAccess(t *testing.T) {
	f := newThreadFixture(t, false)
	if _, err := f.uc.State(context.Background(), operator, f.convID); !errors.Is(err, ErrConversationAccessDenied) {
		t.Fatalf("got %v", err)
	}
}
