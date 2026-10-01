package conversation_usecase

import (
	"errors"
	"testing"

	"vozko/domain/conversation"
	"vozko/domain/shared"
)

type mediaRows struct {
	rows []*conversation.ConversationMedia
	fail error
}

func (m *mediaRows) Create(media *conversation.ConversationMedia) error {
	m.rows = append(m.rows, media)
	return m.fail
}
func (m *mediaRows) GetByID(id string) (*conversation.ConversationMedia, error) {
	for _, r := range m.rows {
		if r.ID == id {
			return r, nil
		}
	}
	return nil, conversation.ErrMediaNotFound
}
func (m *mediaRows) ListByIDs(ids []string) ([]*conversation.ConversationMedia, error) {
	if m.fail != nil {
		return nil, m.fail
	}
	var out []*conversation.ConversationMedia
	for _, id := range ids {
		if r, err := m.GetByID(id); err == nil {
			out = append(out, r)
		}
	}
	return out, nil
}
func (m *mediaRows) GetByWhatsAppMediaID(string) (*conversation.ConversationMedia, error) {
	return nil, conversation.ErrMediaNotFound
}
func (m *mediaRows) ListByEntry(string, shared.EntryType) ([]*conversation.ConversationMedia, error) {
	return m.rows, nil
}
func (m *mediaRows) Delete(string) error                          { return nil }
func (m *mediaRows) DeleteByEntry(string, shared.EntryType) error { return nil }

type countingMediaRows struct {
	mediaRows
	queries int
}

func (c *countingMediaRows) ListByIDs(ids []string) ([]*conversation.ConversationMedia, error) {
	c.queries++
	return c.mediaRows.ListByIDs(ids)
}

func mediaMessage(id, mediaID string) *conversation.Message {
	return &conversation.Message{ID: id, EntryID: "e1", EntryType: shared.EntryTypeWhatsApp, MediaID: &mediaID, SenderName: "Maria"}
}

func TestAHistoryPageCarriesItsMediaFromOneQuery(t *testing.T) {
	rows := &countingMediaRows{mediaRows: mediaRows{rows: []*conversation.ConversationMedia{
		{ID: "m1", EntryID: "e1", EntryType: shared.EntryTypeWhatsApp, URL: "https://cdn/1.jpg", MimeType: "image/jpeg", Layout: conversation.MediaLayout{Width: 800, Height: 600, Thumbhash: "abc"}},
		{ID: "m2", EntryID: "e1", EntryType: shared.EntryTypeWhatsApp, URL: "https://cdn/2.ogg"},
	}}}
	s := &HistoryProviderService{}
	s.SetMediaRepo(rows)
	page := []*conversation.Message{mediaMessage("a", "m1"), {ID: "b", EntryID: "e1", Text: "oi"}, mediaMessage("c", "m2"), mediaMessage("d", "m1")}

	s.attachMedia("e1", shared.EntryTypeWhatsApp, page)

	if rows.queries != 1 {
		t.Fatalf("queries = %d, want one per page", rows.queries)
	}
	if page[0].Media == nil || page[0].Media.URL != "https://cdn/1.jpg" || page[0].Media.Layout.Width != 800 {
		t.Fatalf("media = %+v", page[0].Media)
	}
	if page[1].Media != nil || page[2].Media == nil || page[3].Media == nil {
		t.Fatalf("every message with media gets it, others none: %+v %+v %+v", page[1].Media, page[2].Media, page[3].Media)
	}
}

func TestMediaOfAnotherConversationIsNeverAttached(t *testing.T) {
	rows := &mediaRows{rows: []*conversation.ConversationMedia{{ID: "m1", EntryID: "other", EntryType: shared.EntryTypeWhatsApp, URL: "https://cdn/x.jpg"}}}
	s := &HistoryProviderService{}
	s.SetMediaRepo(rows)
	page := []*conversation.Message{mediaMessage("a", "m1")}
	s.attachMedia("e1", shared.EntryTypeWhatsApp, page)
	if page[0].Media != nil {
		t.Fatal("a media id pointing at another conversation must not expose its file")
	}
}

func TestAPageWithoutMediaDoesNotQuery(t *testing.T) {
	rows := &countingMediaRows{}
	s := &HistoryProviderService{}
	s.SetMediaRepo(rows)
	s.attachMedia("e1", shared.EntryTypeWhatsApp, []*conversation.Message{{ID: "a", Text: "oi"}})
	if rows.queries != 0 {
		t.Fatal("no media, no query")
	}
}

func TestALiveMessageIsPresentedWithItsMedia(t *testing.T) {
	rows := &mediaRows{rows: []*conversation.ConversationMedia{{ID: "m1", EntryID: "e1", EntryType: shared.EntryTypeWhatsApp, URL: "https://cdn/1.jpg"}}}
	s := &HistoryProviderService{}
	s.SetMediaRepo(rows)
	live := mediaMessage("a", "m1")
	s.PresentMessage("e1", string(shared.EntryTypeWhatsApp), live)
	if live.Media == nil || live.Media.URL != "https://cdn/1.jpg" {
		t.Fatalf("media = %+v", live.Media)
	}
}

func TestAnUnreadableMediaTableLeavesTheMessagesAsTheyWere(t *testing.T) {
	s := &HistoryProviderService{}
	s.SetMediaRepo(&mediaRows{fail: errors.New("db down")})
	page := []*conversation.Message{mediaMessage("a", "m1")}
	s.attachMedia("e1", shared.EntryTypeWhatsApp, page)
	if page[0].Media != nil || page[0].MediaID == nil {
		t.Fatal("messages keep their media id so the view can still resolve it")
	}
}
