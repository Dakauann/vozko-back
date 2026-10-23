package choicestats_usecase

import (
	"context"
	"errors"
	"sync"
	"testing"
	"time"

	"vozko/domain/choicestats"
)

type fakeStore struct {
	mu      sync.Mutex
	sends   []choicestats.SendRecord
	clicks  []choicestats.ClickRecord
	sendErr error
}

func (s *fakeStore) RecordSend(_ context.Context, send choicestats.SendRecord) (bool, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.sendErr != nil {
		return false, s.sendErr
	}
	s.sends = append(s.sends, send)
	return true, nil
}

func (s *fakeStore) RecordClick(_ context.Context, click choicestats.ClickRecord) (bool, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.clicks = append(s.clicks, click)
	return true, nil
}

func (s *fakeStore) Counts(context.Context, choicestats.CountQuery) ([]choicestats.CountRow, error) {
	return nil, nil
}

func (s *fakeStore) PrunePrompts(time.Time, int) (int64, error) { return 0, nil }

func (s *fakeStore) PruneCounts(time.Time, int) (int64, error) { return 0, nil }

type fakeZones struct {
	loc   *time.Location
	err   error
	calls int
}

func (z *fakeZones) Location(context.Context, string, string) (*time.Location, error) {
	z.calls++
	return z.loc, z.err
}

type fakeMetrics struct {
	mu       sync.Mutex
	outcomes []string
}

func (m *fakeMetrics) IncChoiceStatsWrite(op, outcome string) {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.outcomes = append(m.outcomes, op+":"+outcome)
}

var saoPaulo = time.FixedZone("BRT", -3*3600)

func aSend() choicestats.Send {
	return choicestats.Send{
		WorkspaceID: "ws", PromptKey: "wf:r:ask#1", ProviderMessageID: "wamid.1",
		Source: choicestats.Source{Kind: choicestats.SourceWorkflow, ID: "wf", NodeID: "ask"},
		Shown:  []string{"sim"}, SentAt: time.Date(2026, 10, 22, 2, 0, 0, 0, time.UTC),
	}
}

var testClickTokens, _ = choicestats.NewClickTokens("server-secret")

func TestARecorderRefusesToBeBuiltWithoutItsStoreZonesOrClickTokens(t *testing.T) {
	if _, err := NewRecorder(nil, &fakeZones{}, &fakeMetrics{}, testClickTokens); !errors.Is(err, ErrRecorderUnwired) {
		t.Fatalf("err = %v", err)
	}
	if _, err := NewRecorder(&fakeStore{}, nil, &fakeMetrics{}, testClickTokens); !errors.Is(err, ErrRecorderUnwired) {
		t.Fatalf("err = %v", err)
	}
	if _, err := NewRecorder(&fakeStore{}, &fakeZones{loc: time.UTC}, nil, testClickTokens); !errors.Is(err, ErrRecorderUnwired) {
		t.Fatalf("err = %v", err)
	}
	if _, err := NewRecorder(&fakeStore{}, &fakeZones{loc: time.UTC}, &fakeMetrics{}, nil); !errors.Is(err, ErrRecorderUnwired) {
		t.Fatalf("err = %v", err)
	}
}

func TestASendIsRecordedOnTheWorkspaceDayAndTheZoneIsReadOncePerWorkspace(t *testing.T) {
	store, zones, metrics := &fakeStore{}, &fakeZones{loc: saoPaulo}, &fakeMetrics{}
	r, err := NewRecorder(store, zones, metrics, testClickTokens)
	if err != nil {
		t.Fatal(err)
	}
	r.RecordSend(context.Background(), aSend())
	second := aSend()
	second.PromptKey = "wf:r:ask#2"
	r.RecordSend(context.Background(), second)
	if len(store.sends) != 2 || !store.sends[0].SentDay.Equal(time.Date(2026, 10, 21, 0, 0, 0, 0, time.UTC)) {
		t.Fatalf("sends = %+v", store.sends)
	}
	if zones.calls != 1 {
		t.Fatalf("zone read %d times for one workspace", zones.calls)
	}
}

func TestAnUnreadableZoneRecordsNothingAndSaysWhy(t *testing.T) {
	store, metrics := &fakeStore{}, &fakeMetrics{}
	for _, zones := range []*fakeZones{{err: errors.New("config down")}, {loc: nil}} {
		r, _ := NewRecorder(store, zones, metrics, testClickTokens)
		r.RecordSend(context.Background(), aSend())
	}
	if len(store.sends) != 0 {
		t.Fatal("a send was recorded on a guessed day")
	}
	if len(metrics.outcomes) != 2 || metrics.outcomes[0] != "send:zone_unknown" || metrics.outcomes[1] != "send:zone_unknown" {
		t.Fatalf("outcomes = %v", metrics.outcomes)
	}
}

func TestAStoreFailureNeverReachesTheSender(t *testing.T) {
	store, metrics := &fakeStore{sendErr: errors.New("db down")}, &fakeMetrics{}
	r, _ := NewRecorder(store, &fakeZones{loc: time.UTC}, metrics, testClickTokens)
	r.RecordSend(context.Background(), aSend())
	held := aSend()
	held.ProviderMessageID = ""
	r.RecordSend(context.Background(), held)
	if len(metrics.outcomes) != 2 || metrics.outcomes[0] != "send:failed" || metrics.outcomes[1] != "send:skipped" {
		t.Fatalf("outcomes = %v", metrics.outcomes)
	}
}

func TestClicksAreRecordedByKeyByMessageAndByTrackedLink(t *testing.T) {
	store, metrics := &fakeStore{}, &fakeMetrics{}
	r, _ := NewRecorder(store, &fakeZones{loc: time.UTC}, metrics, testClickTokens)
	at := time.Date(2026, 10, 22, 12, 0, 0, 0, time.UTC)
	r.RecordClick(context.Background(), choicestats.Click{WorkspaceID: "ws", PromptKey: "wf:r:ask#1", Option: "sim", At: at})
	r.RecordClick(context.Background(), choicestats.Click{WorkspaceID: "ws", ProviderMessageID: "wamid.1", Option: "template_button:0", At: at})
	r.RecordClick(context.Background(), choicestats.Click{WorkspaceID: "ws", Option: "sim", At: at})
	token, _ := testClickTokens.Issue(1, "tpl:attempt-1")
	r.RecordLinkClick(context.Background(), "ws", token, at)
	r.RecordLinkClick(context.Background(), "ws", "not-a-token", at)
	if len(store.clicks) != 3 {
		t.Fatalf("clicks = %+v", store.clicks)
	}
	link := store.clicks[2]
	if link.PromptKey != "tpl:attempt-1" || link.Option != "template_url:1" || link.WorkspaceID != "ws" {
		t.Fatalf("link click = %+v", link)
	}
	if metrics.outcomes[2] != "click:skipped" || metrics.outcomes[4] != "link_click:skipped" {
		t.Fatalf("outcomes = %v", metrics.outcomes)
	}
}

func TestAForgedOrTruncatedLinkTokenCreditsNothing(t *testing.T) {
	store, metrics := &fakeStore{}, &fakeMetrics{}
	r, _ := NewRecorder(store, &fakeZones{loc: time.UTC}, metrics, testClickTokens)
	at := time.Date(2026, 10, 22, 12, 0, 0, 0, time.UTC)
	valid, _ := testClickTokens.Issue(1, "tpl:attempt-1")
	otherSecret, _ := choicestats.NewClickTokens("another-secret")
	forged, _ := otherSecret.Issue(1, "tpl:attempt-1")
	for _, bad := range []string{forged, valid[:len(valid)-3], "1.dHBsOmF0dGVtcHQtMQ", choicestats.UnattributedClickSuffix} {
		r.RecordLinkClick(context.Background(), "ws", bad, at)
	}
	if len(store.clicks) != 0 {
		t.Fatalf("clicks = %+v, a token the server did not issue must credit nothing", store.clicks)
	}
	for _, outcome := range metrics.outcomes {
		if outcome != "link_click:skipped" {
			t.Fatalf("outcomes = %v", metrics.outcomes)
		}
	}
	r.RecordLinkClick(context.Background(), "ws", valid, at)
	if len(store.clicks) != 1 || store.clicks[0].PromptKey != "tpl:attempt-1" || store.clicks[0].Option != "template_url:1" {
		t.Fatalf("clicks = %+v", store.clicks)
	}
}
