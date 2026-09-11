package conversation_usecase

import (
	"context"
	"errors"
	"testing"
	"time"

	"vozko/domain/cache"
	"vozko/domain/conversation"
	lmw "vozko/domain/lead_message_window"
	"vozko/domain/shared"
)

type analysisState struct {
	cache.SharedState
	fields map[string]string
}

func (s *analysisState) HSet(_, field, value string) error { s.fields[field] = value; return nil }
func (s *analysisState) HGetAll(string) (map[string]string, error) {
	out := map[string]string{}
	for k, v := range s.fields {
		out[k] = v
	}
	return out, nil
}
func (s *analysisState) HDelIfValue(_, field, value string) error {
	if s.fields[field] == value {
		delete(s.fields, field)
	}
	return nil
}
func (s *analysisState) SetNX(string, string, time.Duration) (bool, error) { return true, nil }

type subjectQueue func(context.Context, *AnalysisSubject) error

func (q subjectQueue) EnqueueSubject(ctx context.Context, s *AnalysisSubject) error { return q(ctx, s) }

func TestAnalysisDebounceAcknowledgesOnlyCompletedVersion(t *testing.T) {
	for _, mode := range []string{"success", "queue failure", "resolver failure", "new activity"} {
		t.Run(mode, func(t *testing.T) {
			old := encodeAnalysisDebounceValue(shared.EntryTypeUnofficialWhatsApp, time.Now().Add(-6*time.Minute))
			state := &analysisState{fields: map[string]string{"entry": old}}
			calls := 0
			job := &analysisDebounceJob{sharedState: state}
			job.SetAnalysisSubjectResolver(shared.EntryTypeUnofficialWhatsApp, func(context.Context, string) (*AnalysisSubject, error) {
				if mode == "resolver failure" {
					return nil, errors.New("database unavailable")
				}
				return &AnalysisSubject{EntryID: "entry", EntryType: shared.EntryTypeUnofficialWhatsApp, EnableAnalysis: true}, nil
			})
			job.SetAnalysisQueue(subjectQueue(func(_ context.Context, subject *AnalysisSubject) error {
				calls++
				if subject.EntryID != "entry" {
					t.Fatal("lost resolved entry")
				}
				if mode == "queue failure" {
					return errors.New("insert failed")
				}
				if mode == "new activity" {
					state.fields["entry"] = encodeAnalysisDebounceValue(subject.EntryType, time.Now())
				}
				return nil
			}))
			// No transcript repository is installed: an analysis-only handoff
			// must not fetch the same transcript ahead of the analysis adapter.
			if err := job.ProcessPendingAnalyses(); err != nil {
				t.Fatal(err)
			}
			_, retained := state.fields["entry"]
			if retained != (mode != "success") {
				t.Fatalf("retained=%v for %s", retained, mode)
			}
			if mode == "new activity" && state.fields["entry"] == old {
				t.Fatal("lost concurrent activity")
			}
			if mode != "resolver failure" && calls != 1 {
				t.Fatalf("queue calls=%d", calls)
			}
		})
	}
}

func TestAdapterSendsScheduleAnalysisAfterPersistence(t *testing.T) {
	for _, kind := range []shared.EntryType{shared.EntryTypeInstagram, shared.EntryTypeTelegram, shared.EntryTypeUnofficialWhatsApp} {
		for _, open := range []bool{true, false} {
			t.Run(string(kind)+map[bool]string{true: "/open", false: "/closed"}[open], func(t *testing.T) {
				state := &analysisState{fields: map[string]string{}}
				repo := &stubAudioMessageRepo{}
				sender := &MessageSenderService{messageRepo: repo, sharedState: state}
				sender.SetChannelAdapters(conversation.NewAdapterRegistry(&fakeWindowAdapter{entryType: kind, open: open}))
				_, err := sender.SendTextMessage("entry", string(kind), "hello", "operator", "")
				if !open {
					if !errors.Is(err, conversation.ErrOutboundWindowClosed) || len(state.fields) != 0 || len(repo.created) != 0 {
						t.Fatalf("closed window sent or scheduled: %v", err)
					}
					return
				}
				if err != nil {
					t.Fatal(err)
				}
				stamp, ok := decodeAnalysisDebounceValue(state.fields["entry"])
				if !ok || stamp.EntryType != kind || len(repo.created) != 1 {
					t.Fatalf("successful send not scheduled: %#v", state.fields)
				}
			})
		}
	}
}

type inboxWindowMessages struct {
	conversation.MessageRepository
	entry conversation.EntryWithLastMessage
}

func (m inboxWindowMessages) GetEntryLastMessage(string, shared.EntryType) (*conversation.EntryWithLastMessage, error) {
	return &m.entry, nil
}

type officialWindows struct {
	lmw.Repository
	reads  int
	phones []string
	lastAt time.Time
}

func (w *officialWindows) FindByLeadAndBusinessPhone(string, string) (*lmw.LeadMessageWindow, error) {
	w.reads++
	return &lmw.LeadMessageWindow{LastMessageAt: w.lastAt}, nil
}
func (w *officialWindows) FindOpenWindowsByLeadIDs(_ []string, phone string) (map[string]*lmw.LeadMessageWindow, error) {
	w.phones = append(w.phones, phone)
	return map[string]*lmw.LeadMessageWindow{"lead": {LastMessageAt: w.lastAt}}, nil
}

func TestInboxUpdatePreservesChannelWindowsWithoutOfficialLookup(t *testing.T) {
	for _, kind := range []shared.EntryType{shared.EntryTypeInstagram, shared.EntryTypeTelegram, shared.EntryTypeUnofficialWhatsApp} {
		for _, open := range []bool{true, false} {
			t.Run(string(kind)+map[bool]string{true: "/open", false: "/closed"}[open], func(t *testing.T) {
				windows := &officialWindows{lastAt: time.Now()}
				svc := &HistoryProviderService{messageWindowRepo: windows, messageRepo: inboxWindowMessages{entry: conversation.EntryWithLastMessage{
					EntryID: "entry", EntryType: kind, LeadID: "lead", BusinessPhoneID: "instance", LastMessageType: conversation.MessageTypeSystem,
				}}}
				expires := time.Now().Add(time.Hour)
				svc.SetChannelAdapters(conversation.NewAdapterRegistry(&fakeWindowAdapter{entryType: kind, open: open, expiresAt: &expires}))
				entry, err := svc.GetInboxEntry("entry", string(kind))
				if err != nil {
					t.Fatal(err)
				}
				if windows.reads != 0 {
					t.Fatalf("queried official table %d times", windows.reads)
				}
				if entry.WindowOpen != open {
					t.Fatalf("window open=%v want %v", entry.WindowOpen, open)
				}
				if open && (entry.WindowExpiresAt == nil || !entry.WindowExpiresAt.Equal(expires)) {
					t.Fatal("lost channel expiry")
				}
				if !open && entry.WindowClosedReason != string(conversation.WindowReasonExpired) {
					t.Fatal("lost closure reason")
				}
			})
		}
	}
}

func TestOfficialWindowStillUsesIts24HourClock(t *testing.T) {
	for _, age := range []time.Duration{time.Hour, 25 * time.Hour} {
		windows := &officialWindows{lastAt: time.Now().Add(-age)}
		svc := &HistoryProviderService{messageWindowRepo: windows, whatsappRepo: stubAudioEntryRepo{}}
		got := svc.GetWindowStatusForEntry("entry", "whatsapp")
		if got.Open != (age < 24*time.Hour) || windows.reads != 1 {
			t.Fatalf("age=%v state=%+v reads=%d", age, got, windows.reads)
		}
	}
}

func TestWindowBatchQueriesOnlyOfficialNumbers(t *testing.T) {
	windows := &officialWindows{lastAt: time.Now()}
	svc := &HistoryProviderService{messageWindowRepo: windows}
	entries := []conversation.EntryWithLastMessage{}
	for _, kind := range []shared.EntryType{shared.EntryTypeWhatsApp, shared.EntryTypeInstagram, shared.EntryTypeTelegram, shared.EntryTypeUnofficialWhatsApp} {
		entries = append(entries, conversation.EntryWithLastMessage{EntryID: string(kind), EntryType: kind, LeadID: "lead", BusinessPhoneID: string(kind)})
	}
	got := svc.batchGetWindowStatus(entries)
	if len(windows.phones) != 1 || windows.phones[0] != "whatsapp" || len(got) != 1 || !got["whatsapp"].open {
		t.Fatalf("phones=%v windows=%v", windows.phones, got)
	}
}
