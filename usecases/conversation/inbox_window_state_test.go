package conversation_usecase

import (
	"context"
	"errors"
	"testing"
	"time"

	"vozko/domain/conversation"
	lmw "vozko/domain/lead_message_window"
	"vozko/domain/shared"
	wce "vozko/domain/whatsapp_campaign_entry"
)

type batchWindowAdapter struct {
	entryType shared.EntryType
	states    map[string]conversation.WindowState
	err       error

	batches  [][]string
	singles  int
	resolves int
}

func (a *batchWindowAdapter) EntryType() shared.EntryType { return a.entryType }

func (a *batchWindowAdapter) ResolveEntry(_ context.Context, entryID string) (*conversation.EntryContext, error) {
	a.resolves++
	return &conversation.EntryContext{EntryID: entryID, EntryType: a.entryType}, nil
}

func (a *batchWindowAdapter) WindowState(_ context.Context, ec *conversation.EntryContext) (conversation.WindowState, error) {
	a.singles++
	if a.err != nil {
		return conversation.ClosedWindow(conversation.WindowReasonChannelUnavailable), a.err
	}
	if state, ok := a.states[ec.EntryID]; ok {
		return state, nil
	}
	return conversation.ClosedWindow(conversation.WindowReasonChannelUnavailable), nil
}

func (a *batchWindowAdapter) WindowStates(_ context.Context, entryIDs []string) (map[string]conversation.WindowState, error) {
	a.batches = append(a.batches, append([]string(nil), entryIDs...))
	if a.err != nil {
		return nil, a.err
	}
	out := map[string]conversation.WindowState{}
	for _, id := range entryIDs {
		if state, ok := a.states[id]; ok {
			out[id] = state
		}
	}
	return out, nil
}

func (a *batchWindowAdapter) SendText(context.Context, *conversation.EntryContext, conversation.SendTextRequest) (*conversation.SendOutcome, error) {
	return &conversation.SendOutcome{}, nil
}

func (a *batchWindowAdapter) SendMedia(context.Context, *conversation.EntryContext, conversation.SendMediaRequest) (*conversation.SendOutcome, error) {
	return &conversation.SendOutcome{}, nil
}

type singleOnlyAdapter struct{ entryType shared.EntryType }

func (a singleOnlyAdapter) EntryType() shared.EntryType { return a.entryType }
func (a singleOnlyAdapter) ResolveEntry(_ context.Context, id string) (*conversation.EntryContext, error) {
	return &conversation.EntryContext{EntryID: id, EntryType: a.entryType}, nil
}
func (a singleOnlyAdapter) WindowState(context.Context, *conversation.EntryContext) (conversation.WindowState, error) {
	return conversation.OpenWindow(nil), nil
}
func (a singleOnlyAdapter) SendText(context.Context, *conversation.EntryContext, conversation.SendTextRequest) (*conversation.SendOutcome, error) {
	return &conversation.SendOutcome{}, nil
}
func (a singleOnlyAdapter) SendMedia(context.Context, *conversation.EntryContext, conversation.SendMediaRequest) (*conversation.SendOutcome, error) {
	return &conversation.SendOutcome{}, nil
}

type leadWindows struct {
	lmw.Repository
	windows []*lmw.LeadMessageWindow
	err     error
	calls   [][]string
}

func (w *leadWindows) FindByLeadIDs(leadIDs []string) ([]*lmw.LeadMessageWindow, error) {
	w.calls = append(w.calls, append([]string(nil), leadIDs...))
	if w.err != nil {
		return nil, w.err
	}
	wanted := map[string]bool{}
	for _, id := range leadIDs {
		wanted[id] = true
	}
	var out []*lmw.LeadMessageWindow
	for _, window := range w.windows {
		if wanted[window.LeadID] {
			out = append(out, window)
		}
	}
	return out, nil
}

func inboxRow(id string, entryType shared.EntryType) conversation.EntryWithLastMessage {
	return conversation.EntryWithLastMessage{EntryID: id, EntryType: entryType, LastMessageType: conversation.MessageTypeSystem}
}

func officialRow(id, leadID, phoneID string) conversation.EntryWithLastMessage {
	row := inboxRow(id, shared.EntryTypeWhatsApp)
	row.LeadID, row.BusinessPhoneID = leadID, phoneID
	return row
}

func channelEntryTypes() []shared.EntryType {
	return []shared.EntryType{
		shared.EntryTypeInstagram, shared.EntryTypeFacebook, shared.EntryTypeTelegram, shared.EntryTypeWebchat,
		shared.EntryTypeUnofficialWhatsApp, shared.EntryTypeMercadoLivre,
	}
}

func assertRowWindow(t *testing.T, entry conversation.InboxEntry, want conversation.WindowState) {
	t.Helper()
	got := conversation.WindowState{Open: entry.WindowOpen, ExpiresAt: entry.WindowExpiresAt,
		Reason: conversation.WindowClosedReason(entry.WindowClosedReason), ProviderDetail: entry.WindowClosedDetail}
	want.Tier = ""
	if !got.Equal(want) {
		t.Fatalf("%s row window = %+v, want %+v", entry.EntryType, got, want)
	}
}

func TestInboxRowsCarryTheWindowOfEveryChannel(t *testing.T) {
	expires := time.Now().UTC().Add(3 * time.Hour).Truncate(time.Second)
	reported := map[shared.EntryType]conversation.WindowState{
		shared.EntryTypeInstagram:          conversation.OpenWindow(&expires),
		shared.EntryTypeFacebook:           conversation.ClosedWindow(conversation.WindowReasonExpired),
		shared.EntryTypeTelegram:           conversation.ClosedWindow(conversation.WindowReasonReplyRevoked),
		shared.EntryTypeWebchat:            conversation.ClosedWindow(conversation.WindowReasonContactBlocked),
		shared.EntryTypeUnofficialWhatsApp: conversation.ClosedWindowUntil(conversation.WindowReasonAccountRestricted, &expires),
		shared.EntryTypeMercadoLivre:       conversation.ClosedByProvider("blocked_by_buyer"),
	}
	var adapters []conversation.ChannelAdapter
	var rows []conversation.EntryWithLastMessage
	for _, entryType := range channelEntryTypes() {
		id := "entry-" + string(entryType)
		adapters = append(adapters, &batchWindowAdapter{entryType: entryType, states: map[string]conversation.WindowState{id: reported[entryType]}})
		rows = append(rows, inboxRow(id, entryType))
	}
	svc := &HistoryProviderService{}
	svc.SetChannelAdapters(conversation.NewAdapterRegistry(adapters...))

	entries := svc.buildInboxEntries(rows, "")

	for _, entry := range entries {
		assertRowWindow(t, entry, reported[shared.EntryType(entry.EntryType)])
	}
}

func TestInboxWindowsAskEachChannelOncePerPage(t *testing.T) {
	instagram := &batchWindowAdapter{entryType: shared.EntryTypeInstagram, states: map[string]conversation.WindowState{
		"ig-1": conversation.OpenWindow(nil), "ig-2": conversation.OpenWindow(nil), "ig-3": conversation.OpenWindow(nil),
	}}
	telegram := &batchWindowAdapter{entryType: shared.EntryTypeTelegram, states: map[string]conversation.WindowState{
		"tg-1": conversation.OpenWindow(nil), "tg-2": conversation.OpenWindow(nil),
	}}
	svc := &HistoryProviderService{}
	svc.SetChannelAdapters(conversation.NewAdapterRegistry(instagram, telegram))
	rows := []conversation.EntryWithLastMessage{
		inboxRow("ig-1", shared.EntryTypeInstagram), inboxRow("tg-1", shared.EntryTypeTelegram),
		inboxRow("ig-2", shared.EntryTypeInstagram), inboxRow("ig-3", shared.EntryTypeInstagram),
		inboxRow("tg-2", shared.EntryTypeTelegram),
	}

	svc.buildInboxEntries(rows, "")

	if len(instagram.batches) != 1 || len(instagram.batches[0]) != 3 {
		t.Fatalf("instagram batches = %v, want one batch of 3", instagram.batches)
	}
	if len(telegram.batches) != 1 || len(telegram.batches[0]) != 2 {
		t.Fatalf("telegram batches = %v, want one batch of 2", telegram.batches)
	}
	for _, a := range []*batchWindowAdapter{instagram, telegram} {
		if a.singles != 0 || a.resolves != 0 {
			t.Fatalf("%s resolved %d and evaluated %d rows one by one", a.entryType, a.resolves, a.singles)
		}
	}
}

func TestInboxWindowsFailClosed(t *testing.T) {
	cases := []struct {
		name    string
		adapter conversation.ChannelAdapter
	}{
		{"no adapter registered", nil},
		{"adapter without a batch port", singleOnlyAdapter{entryType: shared.EntryTypeInstagram}},
		{"batch lookup failed", &batchWindowAdapter{entryType: shared.EntryTypeInstagram, err: errors.New("db down")}},
		{"entry missing from the batch", &batchWindowAdapter{entryType: shared.EntryTypeInstagram, states: map[string]conversation.WindowState{}}},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			svc := &HistoryProviderService{}
			if tc.adapter != nil {
				svc.SetChannelAdapters(conversation.NewAdapterRegistry(tc.adapter))
			}
			entries := svc.buildInboxEntries([]conversation.EntryWithLastMessage{inboxRow("ig-1", shared.EntryTypeInstagram)}, "")
			assertRowWindow(t, entries[0], conversation.ClosedWindow(conversation.WindowReasonChannelUnavailable))
		})
	}
}

func TestOfficialInboxRowsNameWhyTheWindowIsClosed(t *testing.T) {
	now := time.Now().UTC()
	windows := &leadWindows{windows: []*lmw.LeadMessageWindow{
		{LeadID: "lead-open", BusinessPhoneID: "phone-1", LastMessageAt: now.Add(-time.Hour)},
		{LeadID: "lead-old", BusinessPhoneID: "phone-1", LastMessageAt: now.Add(-30 * time.Hour)},
		{LeadID: "lead-other-phone", BusinessPhoneID: "phone-2", LastMessageAt: now.Add(-time.Hour)},
		{LeadID: "lead-any-phone", BusinessPhoneID: "phone-2", LastMessageAt: now.Add(-time.Hour)},
	}}
	svc := &HistoryProviderService{messageWindowRepo: windows}
	rows := []conversation.EntryWithLastMessage{
		officialRow("wa-open", "lead-open", "phone-1"),
		officialRow("wa-expired", "lead-old", "phone-1"),
		officialRow("wa-silent", "lead-new", "phone-1"),
		officialRow("wa-other-phone", "lead-other-phone", "phone-1"),
		officialRow("wa-any-phone", "lead-any-phone", ""),
		officialRow("wa-no-lead", "", "phone-1"),
	}

	entries := svc.buildInboxEntries(rows, "")

	openUntil := now.Add(-time.Hour).Add(lmw.MessageWindowDuration)
	want := map[string]conversation.WindowState{
		"wa-open":        conversation.OpenWindow(&openUntil),
		"wa-expired":     conversation.ClosedWindow(conversation.WindowReasonExpired),
		"wa-silent":      conversation.ClosedWindow(conversation.WindowReasonNoInbound),
		"wa-other-phone": conversation.ClosedWindow(conversation.WindowReasonNoInbound),
		"wa-any-phone":   conversation.OpenWindow(&openUntil),
		"wa-no-lead":     conversation.ClosedWindow(conversation.WindowReasonChannelUnavailable),
	}
	for _, entry := range entries {
		assertRowWindow(t, entry, want[entry.EntryID])
	}
	if len(windows.calls) != 1 {
		t.Fatalf("lead window lookups = %d, want one for the page", len(windows.calls))
	}
}

func TestOfficialInboxRowsFailClosedWhenTheWindowsCannotBeRead(t *testing.T) {
	svc := &HistoryProviderService{messageWindowRepo: &leadWindows{err: errors.New("db down")}}

	entries := svc.buildInboxEntries([]conversation.EntryWithLastMessage{officialRow("wa-1", "lead-1", "phone-1")}, "")

	assertRowWindow(t, entries[0], conversation.ClosedWindow(conversation.WindowReasonChannelUnavailable))
}

type singleEntryRows struct {
	conversation.MessageRepository
	rows map[string]conversation.EntryWithLastMessage
}

func (m singleEntryRows) GetEntryLastMessage(entryID string, _ shared.EntryType) (*conversation.EntryWithLastMessage, error) {
	row, ok := m.rows[entryID]
	if !ok {
		return nil, nil
	}
	return &row, nil
}

func TestListAndSingleEntryAgreeOnEveryChannel(t *testing.T) {
	expires := time.Now().UTC().Add(2 * time.Hour).Truncate(time.Second)
	states := []conversation.WindowState{
		conversation.OpenWindow(&expires),
		conversation.ClosedWindow(conversation.WindowReasonExpired),
		conversation.ClosedWindow(conversation.WindowReasonBudgetExhausted),
		conversation.ClosedByProvider("blocked_by_mediation"),
		conversation.ClosedWindow(conversation.WindowReasonSessionDown),
		conversation.ClosedWindow(conversation.WindowReasonContactBlocked),
	}
	for i, entryType := range channelEntryTypes() {
		t.Run(string(entryType), func(t *testing.T) {
			id := "entry-" + string(entryType)
			adapter := &batchWindowAdapter{entryType: entryType, states: map[string]conversation.WindowState{id: states[i]}}
			row := inboxRow(id, entryType)
			svc := &HistoryProviderService{messageRepo: singleEntryRows{rows: map[string]conversation.EntryWithLastMessage{id: row}}}
			svc.SetChannelAdapters(conversation.NewAdapterRegistry(adapter))

			listed := svc.buildInboxEntries([]conversation.EntryWithLastMessage{row}, "")[0]
			card, err := svc.GetInboxEntry(id, string(entryType))
			if err != nil {
				t.Fatal(err)
			}
			subscribed := svc.GetWindowStatusForEntry(id, string(entryType))

			assertRowWindow(t, listed, subscribed)
			assertRowWindow(t, *card, subscribed)
		})
	}

	for _, age := range []time.Duration{time.Hour, 30 * time.Hour} {
		t.Run(string(shared.EntryTypeWhatsApp)+"/"+age.String(), func(t *testing.T) {
			windows := &leadWindows{windows: []*lmw.LeadMessageWindow{{LeadID: "lead-1", BusinessPhoneID: "phone-1", LastMessageAt: time.Now().UTC().Add(-age)}}}
			row := officialRow("entry", "lead-1", "phone-1")
			svc := &HistoryProviderService{messageWindowRepo: windows, whatsappRepo: officialEntries{},
				messageRepo: singleEntryRows{rows: map[string]conversation.EntryWithLastMessage{"entry": row}}}

			listed := svc.buildInboxEntries([]conversation.EntryWithLastMessage{row}, "")[0]
			card, err := svc.GetInboxEntry("entry", string(shared.EntryTypeWhatsApp))
			if err != nil {
				t.Fatal(err)
			}
			subscribed := svc.GetWindowStatusForEntry("entry", string(shared.EntryTypeWhatsApp))
			if subscribed.Open != (age < lmw.MessageWindowDuration) {
				t.Fatalf("subscribed = %+v", subscribed)
			}

			assertRowWindow(t, listed, subscribed)
			assertRowWindow(t, *card, subscribed)
		})
	}
}

type officialEntries struct{ wce.Repository }

func (officialEntries) FindByID(id string) (*wce.WhatsAppCampaignEntry, error) {
	return &wce.WhatsAppCampaignEntry{ID: id, LeadID: "lead-1"}, nil
}

func (officialEntries) FindByIDs(ids []string) ([]*wce.WhatsAppCampaignEntry, error) {
	out := make([]*wce.WhatsAppCampaignEntry, 0, len(ids))
	for _, id := range ids {
		out = append(out, &wce.WhatsAppCampaignEntry{ID: id, LeadID: "lead-1"})
	}
	return out, nil
}

func (officialEntries) GetCampaignForEntry(string) (*wce.EntryCampaignInfo, error) {
	return &wce.EntryCampaignInfo{CampaignID: "camp-1", WorkspaceID: "ws-1", BusinessPhoneID: "phone-1"}, nil
}
