package ws

import (
	"context"
	"encoding/json"
	"errors"
	"log"
	"runtime/debug"
	"strings"
	"time"

	"vozko/domain/conversation"
	"vozko/domain/personalinbox"
	"vozko/domain/shared"
)

const personalMarkBroadcastType = "personal_mark"

type PersonalMarkPayload struct {
	EntryID   string `json:"entry_id"`
	EntryType string `json:"entry_type"`
	RequestID string `json:"request_id,omitempty"`
	Pinned    *bool  `json:"pinned,omitempty"`
}

type PersonalMarkView struct {
	PinnedAt       *time.Time `json:"pinned_at,omitempty"`
	UnreadMarkedAt *time.Time `json:"unread_marked_at,omitempty"`
}

type PersonalMarkEntry struct {
	EntryID   string `json:"entry_id"`
	EntryType string `json:"entry_type"`
	PersonalMarkView
}

type PersonalMarkEvent struct {
	EntryID   string            `json:"entry_id"`
	EntryType string            `json:"entry_type"`
	Mark      *PersonalMarkView `json:"mark"`
}

func personalMarkViewOf(mark personalinbox.Mark) *PersonalMarkView {
	if mark.Empty() {
		return nil
	}
	return &PersonalMarkView{PinnedAt: mark.PinnedAt, UnreadMarkedAt: mark.UnreadMarkedAt}
}

func personalMarkEntries(marks []personalinbox.Mark) []PersonalMarkEntry {
	out := make([]PersonalMarkEntry, 0, len(marks))
	for _, mark := range marks {
		if view := personalMarkViewOf(mark); view != nil {
			out = append(out, PersonalMarkEntry{EntryID: mark.EntryID, EntryType: string(mark.EntryType), PersonalMarkView: *view})
		}
	}
	return out
}

func (h *ConversationHub) SetPersonalMarks(marks personalinbox.UseCases) {
	h.personalMarks = marks
}

func (h *ConversationHub) AnnouncePersonalMark(workspaceID string, mark personalinbox.Mark) {
	if h == nil || workspaceID == "" || mark.UserID == "" {
		return
	}
	data, err := json.Marshal(&WSOutgoingMessage{
		Type:    WSEventPersonalMark,
		Payload: PersonalMarkEvent{EntryID: mark.EntryID, EntryType: string(mark.EntryType), Mark: personalMarkViewOf(mark)},
	})
	if err != nil {
		log.Printf("[ConversationHub] personal mark: %v", err)
		return
	}
	recipients := []string{mark.UserID}
	h.sendToUsers(workspaceID, recipients, data)
	h.publishUserPayload(personalMarkBroadcastType, workspaceID, recipients, data)
}

func (h *ConversationHub) pageMarks(userID string, entries []conversation.InboxEntry) []PersonalMarkEntry {
	if h.personalMarks == nil || len(entries) == 0 {
		return []PersonalMarkEntry{}
	}
	refs := make([]shared.EntryRef, 0, len(entries))
	for _, e := range entries {
		refs = append(refs, shared.EntryRef{EntryID: e.EntryID, EntryType: shared.EntryType(e.EntryType)})
	}
	return personalMarkEntries(h.personalMarks.PageMarks(context.Background(), userID, refs))
}

func (h *ConversationHub) clearPersonalUnread(conn *WSConnection, entryID, entryType string) {
	if h.personalMarks == nil {
		return
	}
	by, workspaceID := connPerson(conn), conn.WorkspaceID
	entry := shared.EntryRef{EntryID: strings.TrimSpace(entryID), EntryType: shared.EntryType(strings.ToLower(strings.TrimSpace(entryType)))}
	go h.personalMarks.MarkRead(context.Background(), by, workspaceID, entry)
}

func (p PersonalMarkPayload) entry() shared.EntryRef {
	return shared.EntryRef{
		EntryID:   strings.TrimSpace(p.EntryID),
		EntryType: shared.EntryType(strings.ToLower(strings.TrimSpace(p.EntryType))),
	}
}

func (h *ConversationHub) handlePinConversation(conn *WSConnection, payload json.RawMessage) {
	p, ok := h.decodePersonalMark(conn, payload)
	if !ok {
		return
	}
	if p.Pinned == nil {
		h.sendPersonalMarkError(conn, p, messageActionInvalid, "pinned is required")
		return
	}
	pinned := *p.Pinned
	h.runPersonalMark(conn, p, func(ctx context.Context, by shared.Person, entry shared.EntryRef) error {
		_, err := h.personalMarks.Pin(ctx, by, conn.WorkspaceID, entry, pinned)
		return err
	})
}

func (h *ConversationHub) handleMarkUnread(conn *WSConnection, payload json.RawMessage) {
	p, ok := h.decodePersonalMark(conn, payload)
	if !ok {
		return
	}
	h.runPersonalMark(conn, p, func(ctx context.Context, by shared.Person, entry shared.EntryRef) error {
		_, err := h.personalMarks.MarkUnread(ctx, by, conn.WorkspaceID, entry)
		return err
	})
}

func (h *ConversationHub) handleClearUnreadMark(conn *WSConnection, payload json.RawMessage) {
	p, ok := h.decodePersonalMark(conn, payload)
	if !ok {
		return
	}
	h.runPersonalMark(conn, p, func(ctx context.Context, by shared.Person, entry shared.EntryRef) error {
		_, err := h.personalMarks.ClearUnreadMark(ctx, by, conn.WorkspaceID, entry)
		return err
	})
}

func (h *ConversationHub) decodePersonalMark(conn *WSConnection, payload json.RawMessage) (PersonalMarkPayload, bool) {
	var p PersonalMarkPayload
	if err := json.Unmarshal(payload, &p); err != nil {
		h.sendPersonalMarkError(conn, p, messageActionInvalid, "Invalid personal mark payload")
		return p, false
	}
	if h.personalMarks == nil {
		h.sendPersonalMarkError(conn, p, messageActionUnsupported, "Personal marks are not available on this server")
		return p, false
	}
	if entry := p.entry(); entry.EntryID == "" || entry.EntryType == "" {
		h.sendPersonalMarkError(conn, p, messageActionInvalid, "entry_id and entry_type are required")
		return p, false
	}
	return p, true
}

func (h *ConversationHub) runPersonalMark(conn *WSConnection, p PersonalMarkPayload, act func(context.Context, shared.Person, shared.EntryRef) error) {
	by, entry := connPerson(conn), p.entry()
	go func() {
		defer func() {
			if r := recover(); r != nil {
				log.Printf("[ConversationHub] PANIC in personal mark (entry=%s type=%s user=%s): %v\n%s",
					entry.EntryID, entry.EntryType, conn.UserID, r, debug.Stack())
				h.sendPersonalMarkError(conn, p, messageActionFailed, "internal error saving the personal mark")
			}
		}()
		if err := act(context.Background(), by, entry); err != nil {
			h.sendPersonalMarkError(conn, p, personalMarkErrorCode(err), err.Error())
		}
	}()
}

func (h *ConversationHub) sendPersonalMarkError(conn *WSConnection, p PersonalMarkPayload, code, message string) {
	h.sendMessageActionError(conn, MessageActionPayload{RequestID: p.RequestID, EntryID: p.EntryID, EntryType: p.EntryType}, code, message)
}

func personalMarkErrorCode(err error) string {
	switch {
	case errors.Is(err, conversation.ErrUnauthorized):
		return messageActionNotAllowed
	case errors.Is(err, personalinbox.ErrEntryRequired),
		errors.Is(err, personalinbox.ErrUserRequired):
		return messageActionInvalid
	}
	return messageActionFailed
}
