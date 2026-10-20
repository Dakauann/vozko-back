package conversation_usecase

import (
	"context"
	"errors"
	"log"

	"vozko/domain/conversation"
	lmw "vozko/domain/lead_message_window"
	"vozko/domain/shared"
)

func (s *HistoryProviderService) inboxWindows(rows []conversation.EntryWithLastMessage) map[shared.EntryRef]conversation.WindowState {
	out := make(map[shared.EntryRef]conversation.WindowState, len(rows))
	idsByType := map[shared.EntryType][]string{}
	var official []conversation.EntryWithLastMessage
	for _, row := range rows {
		if row.EntryType == shared.EntryTypeWhatsApp {
			official = append(official, row)
			continue
		}
		idsByType[row.EntryType] = append(idsByType[row.EntryType], row.EntryID)
	}
	for entryID, state := range s.officialWindows(official) {
		out[shared.EntryRef{EntryID: entryID, EntryType: shared.EntryTypeWhatsApp}] = state
	}
	for entryType, ids := range idsByType {
		for entryID, state := range s.channelWindows(entryType, ids) {
			out[shared.EntryRef{EntryID: entryID, EntryType: entryType}] = state
		}
	}
	return out
}

func (s *HistoryProviderService) channelWindows(entryType shared.EntryType, entryIDs []string) map[string]conversation.WindowState {
	batch, ok := s.adapterFor(string(entryType)).(conversation.WindowStatesAdapter)
	if !ok {
		log.Printf("[window] %s has no batch window reader; %d inbox rows shown closed", entryType, len(entryIDs))
		return conversation.WithUnavailable(entryIDs, nil)
	}
	states, err := batch.WindowStates(context.Background(), entryIDs)
	if err != nil {
		log.Printf("[window] %s window lookup failed for %d inbox rows: %v", entryType, len(entryIDs), err)
		return conversation.WithUnavailable(entryIDs, nil)
	}
	out := conversation.WithUnavailable(entryIDs, states)
	missing := 0
	for id := range out {
		if _, reported := states[id]; !reported {
			missing++
		}
	}
	if missing > 0 {
		log.Printf("[window] %s reported no window for %d inbox rows; shown closed", entryType, missing)
	}
	return out
}

func (s *HistoryProviderService) officialWindows(rows []conversation.EntryWithLastMessage) map[string]conversation.WindowState {
	out := make(map[string]conversation.WindowState, len(rows))
	if len(rows) == 0 {
		return out
	}
	leadIDs := make([]string, 0, len(rows))
	for _, row := range rows {
		leadIDs = append(leadIDs, row.LeadID)
	}
	byLead, err := s.officialWindowsByLead(leadIDs)
	if err != nil {
		log.Printf("[window] official window lookup failed for %d inbox rows: %v", len(rows), err)
	}
	for _, row := range rows {
		out[row.EntryID] = officialRowWindow(byLead, err, row.LeadID, row.BusinessPhoneID)
	}
	return out
}

func officialRowWindow(byLead map[string][]*lmw.LeadMessageWindow, lookupErr error, leadID, businessPhoneID string) conversation.WindowState {
	if lookupErr != nil || leadID == "" {
		return conversation.ClosedWindow(conversation.WindowReasonChannelUnavailable)
	}
	return officialWindow(byLead[leadID], businessPhoneID)
}

var errNoLeadWindows = errors.New("conversation: lead message windows are not configured")

func (s *HistoryProviderService) officialWindowsByLead(leadIDs []string) (map[string][]*lmw.LeadMessageWindow, error) {
	if s.messageWindowRepo == nil {
		return nil, errNoLeadWindows
	}
	windows, err := s.messageWindowRepo.FindByLeadIDs(shared.UniqueNonEmpty(leadIDs))
	if err != nil {
		return nil, err
	}
	byLead := make(map[string][]*lmw.LeadMessageWindow, len(windows))
	for _, w := range windows {
		byLead[w.LeadID] = append(byLead[w.LeadID], w)
	}
	return byLead, nil
}

func officialWindow(windows []*lmw.LeadMessageWindow, businessPhoneID string) conversation.WindowState {
	var latest *lmw.LeadMessageWindow
	for _, w := range windows {
		if businessPhoneID != "" && w.BusinessPhoneID != businessPhoneID {
			continue
		}
		if latest == nil || w.LastMessageAt.After(latest.LastMessageAt) {
			latest = w
		}
	}
	if latest == nil {
		return conversation.ClosedWindow(conversation.WindowReasonNoInbound)
	}
	if !latest.IsWindowOpen() {
		return conversation.ClosedWindow(conversation.WindowReasonExpired)
	}
	expiresAt := latest.WindowExpiresAt()
	return conversation.OpenWindow(&expiresAt)
}
