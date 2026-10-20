package ws

import (
	"vozko/delivery/conversationnoteview"
	"vozko/domain/conversationnote"
)

type NoteEvent struct {
	EntryID   string                    `json:"entry_id"`
	EntryType string                    `json:"entry_type"`
	Note      conversationnoteview.Note `json:"note"`
}

type NoteDeletedEvent struct {
	EntryID   string `json:"entry_id"`
	EntryType string `json:"entry_type"`
	NoteID    string `json:"note_id"`
}

func (h *ConversationHub) AnnounceNote(note conversationnote.Authored) {
	if h == nil {
		return
	}
	entryType := string(note.EntryType)
	h.broadcast <- &broadcastMessage{
		entryID:   note.EntryID,
		entryType: entryType,
		event: &WSOutgoingMessage{
			Type:    WSEventNote,
			Payload: NoteEvent{EntryID: note.EntryID, EntryType: entryType, Note: conversationnoteview.Of(note)},
		},
	}
}

func (h *ConversationHub) AnnounceNoteDeleted(note conversationnote.Note) {
	if h == nil {
		return
	}
	entryType := string(note.EntryType)
	h.broadcast <- &broadcastMessage{
		entryID:   note.EntryID,
		entryType: entryType,
		event: &WSOutgoingMessage{
			Type:    WSEventNoteDeleted,
			Payload: NoteDeletedEvent{EntryID: note.EntryID, EntryType: entryType, NoteID: note.ID},
		},
	}
}
