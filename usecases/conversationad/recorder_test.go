package conversationad

import (
	"bytes"
	"context"
	"errors"
	"image"
	"image/color"
	"image/png"
	"sync"
	"testing"
	"time"

	"vozko/domain/conversation"
	"vozko/domain/shared"
)

type originBook struct {
	mu   sync.Mutex
	rows map[string]*conversation.AdOrigin
	fail error
}

func (b *originBook) key(entryID string, entryType shared.EntryType) string {
	return string(entryType) + ":" + entryID
}

func (b *originBook) Claim(origin *conversation.AdOrigin) (bool, error) {
	b.mu.Lock()
	defer b.mu.Unlock()
	if b.fail != nil {
		return false, b.fail
	}
	if b.rows == nil {
		b.rows = map[string]*conversation.AdOrigin{}
	}
	k := b.key(origin.EntryID, origin.EntryType)
	if _, taken := b.rows[k]; taken {
		return false, nil
	}
	copied := *origin
	copied.Image = nil
	b.rows[k] = &copied
	return true, nil
}

func (b *originBook) Get(entryID string, entryType shared.EntryType) (*conversation.AdOrigin, error) {
	b.mu.Lock()
	defer b.mu.Unlock()
	if row, ok := b.rows[b.key(entryID, entryType)]; ok {
		copied := *row
		return &copied, nil
	}
	return nil, b.fail
}

type mediaShelf struct {
	stored []conversation.StoreMediaInput
	rows   map[string]*conversation.ConversationMedia
}

func (m *mediaShelf) Store(in conversation.StoreMediaInput) (*conversation.ConversationMedia, error) {
	m.stored = append(m.stored, in)
	row := &conversation.ConversationMedia{ID: in.ID, EntryID: in.EntryID, EntryType: in.EntryType, Type: in.Type, MimeType: in.MimeType, URL: "https://cdn/" + in.Key, Layout: conversation.MediaLayout{Width: 4, Height: 3}}
	if m.rows == nil {
		m.rows = map[string]*conversation.ConversationMedia{}
	}
	m.rows[in.ID] = row
	return row, nil
}

type fetcher struct {
	data  []byte
	err   error
	calls int
}

func (f *fetcher) Fetch(context.Context, string) ([]byte, error) {
	f.calls++
	return f.data, f.err
}

type broadcasts struct{ sent []string }

func (b *broadcasts) BroadcastAdOrigin(entryID string, _ shared.EntryType, origin *conversation.AdOrigin) {
	b.sent = append(b.sent, entryID+":"+origin.Title)
}

func pngBytes() []byte {
	img := image.NewRGBA(image.Rect(0, 0, 4, 3))
	img.Set(0, 0, color.RGBA{R: 200, A: 255})
	var buf bytes.Buffer
	_ = png.Encode(&buf, img)
	return buf.Bytes()
}

func adFrom(title string) *conversation.AdReferral {
	return &conversation.AdReferral{AdID: "ad-1", Platform: conversation.AdPlatformInstagram, Title: title, ImageURL: "https://scontent/ad.jpg"}
}

func recorderWith(book *originBook, shelf *mediaShelf, f *fetcher, b *broadcasts) *Recorder {
	return NewRecorder(book, shelf, f, b, func() time.Time { return time.Date(2026, 10, 1, 12, 0, 0, 0, time.UTC) })
}

func TestTheFirstAdIsKeptWithItsPictureAndPushedLive(t *testing.T) {
	book, shelf, f, b := &originBook{}, &mediaShelf{}, &fetcher{data: pngBytes()}, &broadcasts{}
	recorderWith(book, shelf, f, b).Record(context.Background(), "e1", shared.EntryTypeWhatsApp, adFrom("Promoção de outubro"))

	origin, _ := book.Get("e1", shared.EntryTypeWhatsApp)
	if origin == nil || origin.Title != "Promoção de outubro" || origin.Platform != conversation.AdPlatformInstagram || origin.ImageMediaID == "" {
		t.Fatalf("origin = %+v", origin)
	}
	if len(shelf.stored) != 1 || shelf.stored[0].MimeType != "image/png" || shelf.stored[0].EntryID != "e1" {
		t.Fatalf("stored = %+v", shelf.stored)
	}
	if len(b.sent) != 1 || b.sent[0] != "e1:Promoção de outubro" {
		t.Fatalf("broadcasts = %v", b.sent)
	}
}

func TestALaterAdNeverReplacesTheFirstOne(t *testing.T) {
	book, shelf, f, b := &originBook{}, &mediaShelf{}, &fetcher{data: pngBytes()}, &broadcasts{}
	recorder := recorderWith(book, shelf, f, b)
	recorder.Record(context.Background(), "e1", shared.EntryTypeWhatsApp, adFrom("Primeiro"))
	recorder.Record(context.Background(), "e1", shared.EntryTypeWhatsApp, adFrom("Segundo"))

	origin, _ := book.Get("e1", shared.EntryTypeWhatsApp)
	if origin.Title != "Primeiro" || f.calls != 1 || len(b.sent) != 1 {
		t.Fatalf("title=%q downloads=%d broadcasts=%v", origin.Title, f.calls, b.sent)
	}
}

func TestAnAdWhosePictureFailsIsStillKept(t *testing.T) {
	for name, f := range map[string]*fetcher{
		"download fails": {err: errors.New("cdn 403")},
		"not an image":   {data: []byte("<html>login</html>")},
	} {
		book, shelf := &originBook{}, &mediaShelf{}
		recorderWith(book, shelf, f, &broadcasts{}).Record(context.Background(), "e1", shared.EntryTypeInstagram, adFrom("Sem foto"))
		origin, _ := book.Get("e1", shared.EntryTypeInstagram)
		if origin == nil || origin.ImageMediaID != "" || len(shelf.stored) != 0 {
			t.Fatalf("%s: origin=%+v stored=%v", name, origin, shelf.stored)
		}
	}
}

func TestAnInlineThumbnailNeedsNoDownload(t *testing.T) {
	book, shelf, f := &originBook{}, &mediaShelf{}, &fetcher{}
	ad := &conversation.AdReferral{Title: "Anúncio", Image: pngBytes()}
	recorderWith(book, shelf, f, &broadcasts{}).Record(context.Background(), "e1", shared.EntryTypeUnofficialWhatsApp, ad)
	if f.calls != 0 || len(shelf.stored) != 1 {
		t.Fatalf("downloads=%d stored=%d", f.calls, len(shelf.stored))
	}
}

func TestSomethingThatIsNotAnAdIsIgnored(t *testing.T) {
	book := &originBook{}
	recorderWith(book, &mediaShelf{}, &fetcher{}, &broadcasts{}).Record(context.Background(), "e1", shared.EntryTypeWhatsApp, &conversation.AdReferral{})
	if len(book.rows) != 0 {
		t.Fatal("an empty referral must not create an origin")
	}
}
