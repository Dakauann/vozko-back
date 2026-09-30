package unofficial_whatsapp

import (
	"context"
	"errors"
	"fmt"
	"sync"
	"testing"

	uw "vozko/domain/unofficial_whatsapp"
	webhook_usecase "vozko/usecases/webhook"
)

const lineJID = "5511965467700@s.whatsapp.net"

type guardHarness struct {
	instances    *fakeInstanceRepo
	provider     *fakeProvider
	guard        *LinkGuard
	candidate    *uw.Instance
	mu           sync.Mutex
	disconnected []string
	statusOf     map[string]*uw.Session
	statusErr    error
}

func newGuardHarness(t *testing.T, others ...*uw.Instance) *guardHarness {
	t.Helper()
	h := &guardHarness{statusOf: map[string]*uw.Session{}}
	h.candidate = &uw.Instance{
		ID: "new", WorkspaceID: "ws-1", ServerID: "srv-a", PhoneNumber: "5511965467700",
		Status: uw.StatusAwaitingScan, InstanceToken: "tok-new",
	}
	h.instances = newFakeInstanceRepo(append(others, h.candidate)...)
	h.provider = &fakeProvider{
		StatusFn: func(_ context.Context, ref uw.InstanceRef) (*uw.Session, error) {
			if h.statusErr != nil {
				return nil, h.statusErr
			}
			return h.statusOf[ref.Token], nil
		},
		DisconnectFn: func(_ context.Context, ref uw.InstanceRef) error {
			h.mu.Lock()
			defer h.mu.Unlock()
			h.disconnected = append(h.disconnected, ref.Token)
			return nil
		},
	}
	h.guard = NewLinkGuard(h.instances, newFakeServerRepo(healthyServer("srv-a", 10, 1)), h.provider)
	return h
}

func liveOn(id, workspace, token string) *uw.Instance {
	return &uw.Instance{
		ID: id, WorkspaceID: workspace, ServerID: "srv-a", PhoneNumber: "5511965467700",
		JID: lineJID, Status: uw.StatusConnected, InstanceToken: token,
	}
}

func TestSessionSyncStoresTheBareJID(t *testing.T) {
	instance := &uw.Instance{ID: "inst-1", Status: uw.StatusConnected}
	repo := newFakeInstanceRepo(instance)
	sync := sessionSync{instances: repo}

	if _, err := sync.apply(context.Background(), instance, &uw.Session{
		State: "connected", Connected: true, JID: "5511965467700:37@s.whatsapp.net",
	}); err != nil {
		t.Fatal(err)
	}

	if got := repo.sessionWrites[0].JID; got != lineJID {
		t.Errorf("stored jid = %q; the device suffix changes on every link and must not be the identity", got)
	}
}

func TestANewScanOnTheSameLineReplacesTheOldLink(t *testing.T) {
	old := liveOn("old", "ws-1", "tok-old")
	h := newGuardHarness(t, old)

	if err := h.guard.Admit(context.Background(), h.candidate, lineJID); err != nil {
		t.Fatalf("admit: %v", err)
	}

	if len(h.disconnected) != 1 || h.disconnected[0] != "tok-old" {
		t.Errorf("disconnected = %v; the old link of the same line must be closed at the provider", h.disconnected)
	}
	if old.Status != uw.StatusDisconnected || old.StatusReason == "" {
		t.Errorf("old = %s (%q), want DISCONNECTED with a reason", old.Status, old.StatusReason)
	}
}

func TestANumberLiveInAnotherWorkspaceRefusesTheNewLink(t *testing.T) {
	foreign := liveOn("foreign", "ws-2", "tok-foreign")
	h := newGuardHarness(t, foreign)
	h.statusOf["tok-foreign"] = &uw.Session{State: "connected", Connected: true, JID: lineJID}

	err := h.guard.Admit(context.Background(), h.candidate, lineJID)

	if !errors.Is(err, uw.ErrNumberAlreadyLinked) {
		t.Fatalf("err = %v, want ErrNumberAlreadyLinked", err)
	}
	if len(h.disconnected) != 1 || h.disconnected[0] != "tok-new" {
		t.Errorf("disconnected = %v; only the refused new link may be closed, never another tenant's", h.disconnected)
	}
	if h.candidate.Status != uw.StatusDisconnected || h.candidate.StatusReason == "" {
		t.Errorf("candidate = %s (%q)", h.candidate.Status, h.candidate.StatusReason)
	}
	if foreign.Status != uw.StatusConnected {
		t.Error("another workspace's live number must be left alone")
	}
}

func TestAStaleLiveRecordElsewhereIsCorrectedAndTheNewLinkAdmitted(t *testing.T) {
	stale := liveOn("stale", "ws-2", "tok-stale")
	h := newGuardHarness(t, stale)
	h.statusOf["tok-stale"] = &uw.Session{State: "disconnected"}

	if err := h.guard.Admit(context.Background(), h.candidate, lineJID); err != nil {
		t.Fatalf("admit: %v", err)
	}
	if stale.Status != uw.StatusDisconnected {
		t.Errorf("stale = %s; a record the provider no longer backs must be corrected", stale.Status)
	}
	if len(h.disconnected) != 0 {
		t.Errorf("disconnected = %v; nothing live needed closing", h.disconnected)
	}
}

func TestAnUnreachableProviderHoldsTheNewLinkWithoutClosingAnything(t *testing.T) {
	h := newGuardHarness(t, liveOn("foreign", "ws-2", "tok-foreign"))
	h.statusErr = &uw.ProviderError{HTTPStatus: 502, Message: "bad gateway"}

	err := h.guard.Admit(context.Background(), h.candidate, lineJID)

	if err == nil || errors.Is(err, uw.ErrNumberAlreadyLinked) {
		t.Errorf("err = %v; an unanswered probe must hold the link for the next poll, not refuse it", err)
	}
	if len(h.disconnected) != 0 {
		t.Errorf("disconnected = %v", h.disconnected)
	}
}

func TestSessionSyncAsksTheGuardBeforeGoingLive(t *testing.T) {
	foreign := liveOn("foreign", "ws-2", "tok-foreign")
	h := newGuardHarness(t, foreign)
	h.statusOf["tok-foreign"] = &uw.Session{State: "connected", Connected: true, JID: lineJID}
	sync := sessionSync{instances: h.instances, gate: h.guard}

	_, err := sync.apply(context.Background(), h.candidate, &uw.Session{
		State: "connected", Connected: true, JID: "5511965467700:37@s.whatsapp.net",
	})

	if !errors.Is(err, uw.ErrNumberAlreadyLinked) {
		t.Errorf("err = %v; a refused link must not be recorded as connected", err)
	}
	if h.candidate.Status != uw.StatusDisconnected {
		t.Errorf("candidate = %s; a refused link must end disconnected, never recorded as connected", h.candidate.Status)
	}
}

func TestARefusedLinkIsDroppedNotRetried(t *testing.T) {
	err := fmt.Errorf("apply: %w", uw.ErrNumberAlreadyLinked)
	if got := classifyWebhookFailure(err); got != webhook_usecase.DispositionDrop {
		t.Errorf("disposition = %v; retrying a refused link would re-run the refusal every few seconds", got)
	}
}

func TestARevivedOlderSessionDoesNotKickTheNewerLink(t *testing.T) {
	newer := liveOn("newer", "ws-1", "tok-newer")
	h := newGuardHarness(t, newer)
	h.candidate.Status = uw.StatusDisconnected
	h.statusOf["tok-newer"] = &uw.Session{State: "connected", Connected: true, JID: lineJID}

	err := h.guard.Admit(context.Background(), h.candidate, lineJID)

	if !errors.Is(err, uw.ErrNumberAlreadyLinked) {
		t.Fatalf("err = %v; only a fresh scan may replace the live link", err)
	}
	if newer.Status != uw.StatusConnected {
		t.Error("the newer live link was closed by a background revival of the old one")
	}
	if len(h.disconnected) != 1 || h.disconnected[0] != "tok-new" {
		t.Errorf("disconnected = %v; the revived old session is the one to close", h.disconnected)
	}
}
