package smartcampaign_usecase

import (
	"context"
	"sync"
	"testing"
	"time"

	wce "vozko/domain/whatsapp_campaign_entry"
)

type entryRepo struct {
	wce.Repository
	entries map[string]*wce.WhatsAppCampaignEntry
	updated []string
	failErr error
}

func (r *entryRepo) UpdateStatus(entryID string, status wce.SendStatus, _ string, code int, _ string) error {
	if r.failErr != nil {
		return r.failErr
	}
	r.updated = append(r.updated, entryID+":"+string(status))
	return nil
}

func (r *entryRepo) FindByID(entryID string) (*wce.WhatsAppCampaignEntry, error) {
	return r.entries[entryID], nil
}

type failureSink struct {
	mu   sync.Mutex
	seen []string
	done chan struct{}
}

func (s *failureSink) EntryFailed(_ context.Context, campaignID string, code int) error {
	s.mu.Lock()
	s.seen = append(s.seen, campaignID)
	s.mu.Unlock()
	if code == 131048 {
		close(s.done)
	}
	return nil
}

func TestAFailedEntryIsReportedWithItsCampaign(t *testing.T) {
	repo := &entryRepo{entries: map[string]*wce.WhatsAppCampaignEntry{"e1": {ID: "e1", CampaignID: "c1"}}}
	sink := &failureSink{done: make(chan struct{})}
	observed := ObserveFailures(repo)
	observed.Attach(sink)

	if err := observed.UpdateStatus("e1", wce.SendStatusFailed, "", 131048, "spam"); err != nil {
		t.Fatal(err)
	}

	select {
	case <-sink.done:
	case <-time.After(2 * time.Second):
		t.Fatal("the failure never reached the sink")
	}
	if len(repo.updated) != 1 || sink.seen[0] != "c1" {
		t.Fatalf("updated %v, sink saw %v", repo.updated, sink.seen)
	}
}

func TestOtherStatusesAndFailedWritesAreNotReported(t *testing.T) {
	repo := &entryRepo{entries: map[string]*wce.WhatsAppCampaignEntry{"e1": {ID: "e1", CampaignID: "c1"}}}
	sink := &failureSink{done: make(chan struct{})}
	observed := ObserveFailures(repo)
	observed.Attach(sink)

	_ = observed.UpdateStatus("e1", wce.SendStatusDelivered, "m", 0, "")
	repo.failErr = errBroken
	if err := observed.UpdateStatus("e1", wce.SendStatusFailed, "", 131048, ""); err == nil {
		t.Fatal("the write error was swallowed")
	}
	time.Sleep(50 * time.Millisecond)

	if len(sink.seen) != 0 {
		t.Fatalf("sink saw %v, want nothing", sink.seen)
	}
}

func TestWithoutASinkTheRepositoryIsUnchanged(t *testing.T) {
	repo := &entryRepo{entries: map[string]*wce.WhatsAppCampaignEntry{}}
	if err := ObserveFailures(repo).UpdateStatus("e1", wce.SendStatusFailed, "", 131026, ""); err != nil || len(repo.updated) != 1 {
		t.Fatalf("err %v, updated %v", err, repo.updated)
	}
}
