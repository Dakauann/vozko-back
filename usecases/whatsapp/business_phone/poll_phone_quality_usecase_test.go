package businessphone_usecase

import (
	"context"
	"errors"
	"testing"
	"time"

	businessphone "vozko/domain/whatsapp/business_phone"
	"vozko/domain/whatsapp/quality"
)

type accountsMeta struct {
	*mockMetaAPIService
	byWABA map[string][]businessphone.MetaPhoneNumberInfo
	failed map[string]error
	asked  []string
}

func (m *accountsMeta) ListPhoneNumbers(wabaID string, _ string) ([]businessphone.MetaPhoneNumberInfo, error) {
	m.asked = append(m.asked, wabaID)
	if err := m.failed[wabaID]; err != nil {
		return nil, err
	}
	return m.byWABA[wabaID], nil
}

type fakeSending struct{ phones []string }

func (f fakeSending) PhonesSendingSince(context.Context, time.Time) ([]string, error) {
	return f.phones, nil
}

type fakeHistory struct{ latest map[quality.Signal]map[string]string }

func (f fakeHistory) LatestValues(_ quality.Subject, signal quality.Signal, _ []string) (map[string]string, error) {
	return f.latest[signal], nil
}

var pollNow = time.Date(2026, 10, 15, 21, 0, 0, 0, time.UTC)

func pollPhone(repo *mockRepository, id, metaID, wabaID string) {
	repo.phoneNumbers[id] = &businessphone.WhatsAppBusinessPhoneNumber{
		ID: id, MetaPhoneNumberID: metaID, WABAId: wabaID, Provider: businessphone.ProviderMeta,
		AccessToken: "token-" + id, Status: businessphone.StatusConnected,
	}
}

func newPhonePoll(repo *mockRepository, meta *accountsMeta, sending []string, history fakeHistory, recorder *fakeQualityRecorder) *PollPhoneQualityUseCase {
	uc := NewPollPhoneQualityUseCase(repo, meta, fakeSending{phones: sending}, history, recorder)
	uc.now = func() time.Time { return pollNow }
	return uc
}

func TestPollAsksOncePerAccountAndRecordsOnlyWhatMoved(t *testing.T) {
	repo := newMockRepo()
	pollPhone(repo, "p1", "m1", "waba-1")
	pollPhone(repo, "p2", "m2", "waba-1")
	pollPhone(repo, "p3", "m3", "waba-2")
	meta := &accountsMeta{mockMetaAPIService: newMockMetaAPI(), byWABA: map[string][]businessphone.MetaPhoneNumberInfo{
		"waba-1": {{ID: "m1", DisplayPhoneNumber: "+55 11 1", QualityRating: "RED", Status: "CONNECTED"}, {ID: "m2", QualityRating: "GREEN", Status: "CONNECTED"}},
		"waba-2": {{ID: "m3", QualityRating: "YELLOW", Status: "FLAGGED"}},
	}}
	history := fakeHistory{latest: map[quality.Signal]map[string]string{
		quality.SignalQuality: {"m1": "GREEN", "m2": "GREEN"},
		quality.SignalStatus:  {"m1": "CONNECTED", "m2": "CONNECTED"},
	}}
	recorder := &fakeQualityRecorder{}

	if err := newPhonePoll(repo, meta, []string{"m1", "m2", "m3"}, history, recorder).Execute(context.Background()); err != nil {
		t.Fatalf("Execute: %v", err)
	}

	if len(meta.asked) != 2 {
		t.Fatalf("one request per account, asked %v", meta.asked)
	}
	got := map[string]quality.Change{}
	for _, c := range recorder.changes {
		got[c.ExternalID+"/"+string(c.Signal)] = c
	}
	if len(got) != 3 {
		t.Fatalf("m1 turned red, m3 is seen for the first time (quality and status); m2 did not move: %+v", recorder.changes)
	}
	if c := got["m1/quality"]; c.Previous != "GREEN" || c.Current != "RED" || c.Source != quality.SourcePoll || !c.OccurredAt.Equal(pollNow) || c.WABAID != "waba-1" {
		t.Fatalf("m1 change %+v", c)
	}
	if c := got["m3/status"]; c.Previous != "" || c.Current != "FLAGGED" {
		t.Fatalf("a first reading starts the history, got %+v", c)
	}
}

func TestPollNeverAsksAboutIdleOrIneligiblePhones(t *testing.T) {
	repo := newMockRepo()
	pollPhone(repo, "idle", "m-idle", "waba-idle")
	pollPhone(repo, "banned", "m-banned", "waba-banned")
	repo.phoneNumbers["banned"].Status = businessphone.StatusBanned
	pollPhone(repo, "dialog", "m-dialog", "waba-dialog")
	repo.phoneNumbers["dialog"].Provider = businessphone.ProviderDialog360
	meta := &accountsMeta{mockMetaAPIService: newMockMetaAPI()}

	err := newPhonePoll(repo, meta, []string{"m-banned", "m-dialog"}, fakeHistory{}, &fakeQualityRecorder{}).Execute(context.Background())

	if err != nil || len(meta.asked) != 0 {
		t.Fatalf("err %v, asked %v", err, meta.asked)
	}
}

func TestPollKeepsOtherAccountsWhenOneFailsAndReportsIt(t *testing.T) {
	boom := errors.New("meta 500")
	repo := newMockRepo()
	pollPhone(repo, "p1", "m1", "waba-1")
	pollPhone(repo, "p2", "m2", "waba-2")
	meta := &accountsMeta{mockMetaAPIService: newMockMetaAPI(),
		byWABA: map[string][]businessphone.MetaPhoneNumberInfo{"waba-2": {{ID: "m2", QualityRating: "GREEN", Status: "CONNECTED"}}},
		failed: map[string]error{"waba-1": boom}}
	recorder := &fakeQualityRecorder{}

	err := newPhonePoll(repo, meta, []string{"m1", "m2"}, fakeHistory{}, recorder).Execute(context.Background())

	if !errors.Is(err, boom) {
		t.Fatalf("the failure is reported, got %v", err)
	}
	if len(recorder.changes) != 2 || recorder.changes[0].ExternalID != "m2" {
		t.Fatalf("the healthy account is still recorded, got %+v", recorder.changes)
	}
}

func TestPollDoesNothingWithoutSendingPhones(t *testing.T) {
	meta := &accountsMeta{mockMetaAPIService: newMockMetaAPI()}

	if err := newPhonePoll(newMockRepo(), meta, nil, fakeHistory{}, &fakeQualityRecorder{}).Execute(context.Background()); err != nil || len(meta.asked) != 0 {
		t.Fatalf("err %v, asked %v", err, meta.asked)
	}
}
