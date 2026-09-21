package conversation_usecase

import (
	"errors"
	"testing"
	"time"

	"vozko/domain/cache"
	"vozko/domain/shared"
)

type stubScheduleState struct {
	cache.SharedState
	fields map[string]string
	err    error
}

func (s *stubScheduleState) HGetAll(string) (map[string]string, error) {
	if s.err != nil {
		return nil, s.err
	}
	return s.fields, nil
}

func stampedAt(entryType shared.EntryType, at time.Time) string {
	return encodeAnalysisDebounceValue(entryType, at)
}

func TestAwaitingAnalysisReportsStampedConversations(t *testing.T) {
	now := time.Now()
	reader := NewAnalysisScheduleReader(&stubScheduleState{fields: map[string]string{
		"entry-1": stampedAt(shared.EntryTypeUnofficialWhatsApp, now),
		"entry-3": stampedAt(shared.EntryTypeUnofficialWhatsApp, now),
		"entry-9": stampedAt(shared.EntryTypeUnofficialWhatsApp, now),
	}})

	got, err := reader.AwaitingAnalysis(
		[]string{"entry-1", "entry-2", "entry-3"}, string(shared.EntryTypeUnofficialWhatsApp))
	if err != nil {
		t.Fatalf("AwaitingAnalysis: %v", err)
	}
	if !got["entry-1"] || !got["entry-3"] {
		t.Errorf("stamped conversations were not reported: %v", got)
	}
	if got["entry-2"] {
		t.Error("an unstamped conversation was reported as awaiting analysis")
	}
	if len(got) != 2 {
		t.Errorf("map holds %d entries, want only the two asked for that are stamped", len(got))
	}
}

func TestAwaitingAnalysisIgnoresAnotherChannelsStamp(t *testing.T) {
	reader := NewAnalysisScheduleReader(&stubScheduleState{fields: map[string]string{
		"entry-1": stampedAt(shared.EntryTypeUnofficialWhatsApp, time.Now()),
	}})

	got, err := reader.AwaitingAnalysis([]string{"entry-1"}, string(shared.EntryTypeTelegram))
	if err != nil {
		t.Fatal(err)
	}
	if got["entry-1"] {
		t.Error("a stamp from another channel was reported for this page")
	}
}

func TestAwaitingAnalysisDegradesWhenTheCacheIsDown(t *testing.T) {
	reader := NewAnalysisScheduleReader(&stubScheduleState{err: errors.New("redis down")})

	got, err := reader.AwaitingAnalysis([]string{"entry-1"}, string(shared.EntryTypeTelegram))
	if err != nil {
		t.Errorf("a cache failure became an inbox failure: %v", err)
	}
	if len(got) != 0 {
		t.Errorf("got %v, want nothing reported", got)
	}
}

func TestAwaitingAnalysisSkipsUndecodableValues(t *testing.T) {
	reader := NewAnalysisScheduleReader(&stubScheduleState{fields: map[string]string{
		"entry-1": "not-a-stamp",
		"entry-2": "",
	}})

	got, err := reader.AwaitingAnalysis([]string{"entry-1", "entry-2"}, string(shared.EntryTypeUnofficialWhatsApp))
	if err != nil {
		t.Fatal(err)
	}
	if len(got) != 0 {
		t.Errorf("got %v, want nothing reported", got)
	}
}

func TestAwaitingAnalysisHandlesEmptyInput(t *testing.T) {
	reader := NewAnalysisScheduleReader(&stubScheduleState{fields: map[string]string{}})
	if got, err := reader.AwaitingAnalysis(nil, "whatsapp"); err != nil || len(got) != 0 {
		t.Errorf("empty page: got %v, %v", got, err)
	}
	if got, err := NewAnalysisScheduleReader(nil).AwaitingAnalysis([]string{"e"}, "whatsapp"); err != nil || len(got) != 0 {
		t.Errorf("no shared state: got %v, %v", got, err)
	}
}
