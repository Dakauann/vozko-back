package conversationad

import (
	"context"
	"errors"
	"testing"

	"vozko/domain/conversation"
	"vozko/domain/shared"
)

type recordedHistory struct {
	records []conversation.MessageHistoryRecord
	fail    error
}

func (h *recordedHistory) Record(_ context.Context, record conversation.MessageHistoryRecord) error {
	h.records = append(h.records, record)
	return h.fail
}

func TestAContactMessageFromAnAdRecordsTheAdAfterTheMessage(t *testing.T) {
	inner, book := &recordedHistory{}, &originBook{}
	history := WithAdOrigins(inner, recorderWith(book, &mediaShelf{}, &fetcher{}, &broadcasts{}))
	err := history.Record(context.Background(), conversation.MessageHistoryRecord{
		SentBy: conversation.SentByContact("5584"), EntryID: "e1", EntryType: shared.EntryTypeWhatsApp, Text: "Oi, vi o anúncio",
		AdReferral: &conversation.AdReferral{AdID: "ad-1", Title: "Promoção"},
	})
	if err != nil || len(inner.records) != 1 {
		t.Fatalf("err=%v records=%d", err, len(inner.records))
	}
	if origin, _ := book.Get("e1", shared.EntryTypeWhatsApp); origin == nil || origin.Title != "Promoção" {
		t.Fatalf("origin = %+v", origin)
	}
}

func TestAMessageThatFailedToSaveRecordsNoAd(t *testing.T) {
	book := &originBook{}
	history := WithAdOrigins(&recordedHistory{fail: errors.New("db down")}, recorderWith(book, &mediaShelf{}, &fetcher{}, &broadcasts{}))
	_ = history.Record(context.Background(), conversation.MessageHistoryRecord{
		SentBy: conversation.SentByContact("5584"), EntryID: "e1", EntryType: shared.EntryTypeWhatsApp,
		AdReferral: &conversation.AdReferral{AdID: "ad-1", Title: "Promoção"},
	})
	if len(book.rows) != 0 {
		t.Fatal("no ad without the message")
	}
}

func TestReadingTheAdAttachesItsStoredPicture(t *testing.T) {
	book, shelf := &originBook{}, &mediaShelf{}
	recorderWith(book, shelf, &fetcher{data: pngBytes()}, &broadcasts{}).Record(context.Background(), "e1", shared.EntryTypeWhatsApp, adFrom("Promoção"))
	origin, err := NewReader(book, mediaRowsOf(shelf)).AdOrigin("e1", shared.EntryTypeWhatsApp)
	if err != nil || origin == nil || origin.Image == nil || origin.Image.Layout.Width != 4 {
		t.Fatalf("origin=%+v err=%v", origin, err)
	}
	none, err := NewReader(book, mediaRowsOf(shelf)).AdOrigin("other", shared.EntryTypeWhatsApp)
	if err != nil || none != nil {
		t.Fatalf("a conversation without an ad reads as none: %+v %v", none, err)
	}
}

type shelfRows struct{ shelf *mediaShelf }

func mediaRowsOf(shelf *mediaShelf) conversation.ConversationMediaRepository {
	return shelfRows{shelf: shelf}
}

func (s shelfRows) Create(*conversation.ConversationMedia) error { return nil }
func (s shelfRows) GetByID(id string) (*conversation.ConversationMedia, error) {
	if row, ok := s.shelf.rows[id]; ok {
		return row, nil
	}
	return nil, conversation.ErrMediaNotFound
}
func (s shelfRows) ListByIDs([]string) ([]*conversation.ConversationMedia, error) { return nil, nil }
func (s shelfRows) GetByWhatsAppMediaID(string) (*conversation.ConversationMedia, error) {
	return nil, conversation.ErrMediaNotFound
}
func (s shelfRows) ListByEntry(string, shared.EntryType) ([]*conversation.ConversationMedia, error) {
	return nil, nil
}
func (s shelfRows) Delete(string) error                          { return nil }
func (s shelfRows) DeleteByEntry(string, shared.EntryType) error { return nil }
