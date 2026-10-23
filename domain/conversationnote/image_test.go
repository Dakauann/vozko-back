package conversationnote

import (
	"bytes"
	"errors"
	"strings"
	"testing"

	"vozko/domain/conversation"
	"vozko/domain/shared"
)

var (
	pngBytes  = []byte("\x89PNG\r\n\x1a\n\x00\x00\x00\rIHDR")
	jpegBytes = []byte("\xff\xd8\xff\xe0\x00\x10JFIF\x00")
	gifBytes  = []byte("GIF89a\x01\x00\x01\x00")
	webpBytes = []byte("RIFF\x24\x00\x00\x00WEBPVP8 ")
)

func TestSniffImageTrustsTheBytesNotTheName(t *testing.T) {
	cases := []struct {
		name string
		data []byte
		mime string
		ext  string
	}{
		{"png", pngBytes, "image/png", ".png"},
		{"jpeg", jpegBytes, "image/jpeg", ".jpg"},
		{"gif", gifBytes, "image/gif", ".gif"},
		{"webp", webpBytes, "image/webp", ".webp"},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			got, err := SniffImage(c.data)
			if err != nil {
				t.Fatalf("SniffImage: %v", err)
			}
			if got.MIME != c.mime || got.Ext != c.ext {
				t.Fatalf("sniffed %+v, want %s %s", got, c.mime, c.ext)
			}
		})
	}
}

func TestSniffImageRefusesWhatIsNotAnAllowedImage(t *testing.T) {
	cases := []struct {
		name string
		data []byte
		want error
	}{
		{"empty", nil, ErrImageRequired},
		{"html dressed as an image", []byte("<html><script>alert(1)</script></html>"), ErrImageType},
		{"svg can carry scripts", []byte(`<svg xmlns="http://www.w3.org/2000/svg"><script/></svg>`), ErrImageType},
		{"pdf", []byte("%PDF-1.7\n"), ErrImageType},
		{"one byte over the limit", append(append([]byte{}, pngBytes...), bytes.Repeat([]byte{0}, MaxImageBytes+1-len(pngBytes))...), ErrImageTooLarge},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			if _, err := SniffImage(c.data); !errors.Is(err, c.want) {
				t.Fatalf("err = %v, want %v", err, c.want)
			}
		})
	}
}

func TestAnImageOfExactlyTheLimitIsAccepted(t *testing.T) {
	data := append(append([]byte{}, pngBytes...), bytes.Repeat([]byte{0}, MaxImageBytes-len(pngBytes))...)
	if _, err := SniffImage(data); err != nil {
		t.Fatalf("an image of %d bytes must be accepted: %v", MaxImageBytes, err)
	}
}

func TestTheStoredFileNameCarriesTheSniffedExtension(t *testing.T) {
	png := SniffedImage{MIME: "image/png", Ext: ".png"}
	cases := []struct {
		original string
		want     string
	}{
		{"print da tela.jpg", "print da tela.png"},
		{"payload.html", "payload.png"},
		{`C:\fakepath\conta.png`, "conta.png"},
		{"../../etc/passwd", "passwd.png"},
		{"", "imagem.png"},
		{".png", "imagem.png"},
		{strings.Repeat("a", 120) + ".gif", strings.Repeat("a", 80) + ".png"},
	}
	for _, c := range cases {
		if got := png.FileName(c.original); got != c.want {
			t.Fatalf("FileName(%q) = %q, want %q", c.original, got, c.want)
		}
	}
}

func TestNormalizeImageIDsCountsDistinctIDsAgainstTheBound(t *testing.T) {
	if got, err := NormalizeImageIDs([]string{" a", "b", "a ", ""}); err != nil || strings.Join(got, ",") != "a,b" {
		t.Fatalf("got %v %v", got, err)
	}
	if got, err := NormalizeImageIDs(nil); err != nil || got == nil || len(got) != 0 {
		t.Fatalf("nil must become an empty list, got %#v %v", got, err)
	}
	if _, err := NormalizeImageIDs(distinctIDs(MaxImages)); err != nil {
		t.Fatalf("exactly %d images must be accepted: %v", MaxImages, err)
	}
	if _, err := NormalizeImageIDs(distinctIDs(MaxImages + 1)); !errors.Is(err, ErrTooManyImages) {
		t.Fatalf("err = %v, want ErrTooManyImages", err)
	}
}

func TestANoteMayBeOnlyImagesButNeverEmpty(t *testing.T) {
	d := validDraft()
	d.Body = "  "
	d.ImageIDs = []string{"img-1"}
	note, err := New(d, "note-1", writtenAt)
	if err != nil {
		t.Fatalf("an image-only note must be accepted: %v", err)
	}
	if note.Body != "" || strings.Join(note.ImageIDs, ",") != "img-1" {
		t.Fatalf("note = %+v", note)
	}

	d.ImageIDs = nil
	if _, err := New(d, "note-1", writtenAt); !errors.Is(err, ErrBodyRequired) {
		t.Fatalf("err = %v, want ErrBodyRequired", err)
	}

	d.Body = "texto"
	d.ImageIDs = distinctIDs(MaxImages + 1)
	if _, err := New(d, "note-1", writtenAt); !errors.Is(err, ErrTooManyImages) {
		t.Fatalf("err = %v, want ErrTooManyImages", err)
	}
}

func TestANoteWithoutImagesStoresAnEmptyList(t *testing.T) {
	note, err := New(validDraft(), "note-1", writtenAt)
	if err != nil {
		t.Fatal(err)
	}
	if note.ImageIDs == nil {
		t.Fatal("image ids must be an empty list, never null")
	}
}

func storedImage(id string) *conversation.ConversationMedia {
	return &conversation.ConversationMedia{
		ID: id, EntryID: chat.EntryID, EntryType: chat.EntryType, Type: conversation.MediaTypeImage,
		MimeType: "image/png", URL: "https://cdn/" + id + ".png", SizeBytes: 2048,
		Layout: conversation.MediaLayout{Width: 800, Height: 600, Thumbhash: "abc"},
	}
}

func TestAttachableOnlyAcceptsAnImageOfTheSameConversationWithinTheRules(t *testing.T) {
	cases := []struct {
		name   string
		change func(*conversation.ConversationMedia)
		want   error
	}{
		{"an image of this conversation", func(*conversation.ConversationMedia) {}, nil},
		{"another conversation", func(m *conversation.ConversationMedia) { m.EntryID = "entry-2" }, ErrImageNotFound},
		{"same id on another channel", func(m *conversation.ConversationMedia) { m.EntryType = shared.EntryTypeTelegram }, ErrImageNotFound},
		{"a document", func(m *conversation.ConversationMedia) { m.Type = conversation.MediaTypeDocument }, ErrImageNotFound},
		{"a sticker", func(m *conversation.ConversationMedia) { m.Type = conversation.MediaTypeSticker }, ErrImageNotFound},
		{"an svg stored as an image", func(m *conversation.ConversationMedia) { m.MimeType = "image/svg+xml" }, ErrImageType},
		{"over the size limit", func(m *conversation.ConversationMedia) { m.SizeBytes = MaxImageBytes + 1 }, ErrImageTooLarge},
		{"no recorded size", func(m *conversation.ConversationMedia) { m.SizeBytes = 0 }, ErrImageTooLarge},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			m := storedImage("img-1")
			c.change(m)
			if err := Attachable(m, chat); !errors.Is(err, c.want) {
				t.Fatalf("err = %v, want %v", err, c.want)
			}
		})
	}
	if err := Attachable(nil, chat); !errors.Is(err, ErrImageNotFound) {
		t.Fatalf("a missing image: err = %v", err)
	}
}

func TestAttachedImagesKeepTheNoteOrderAndDropWhatNoLongerQualifies(t *testing.T) {
	foreign := storedImage("img-foreign")
	foreign.EntryID = "entry-2"
	got := AttachedImages(
		[]string{"img-2", "img-gone", "img-1", "img-foreign"},
		[]*conversation.ConversationMedia{storedImage("img-1"), foreign, nil, storedImage("img-2")},
		chat,
	)
	if len(got) != 2 || got[0].ID != "img-2" || got[1].ID != "img-1" {
		t.Fatalf("images = %+v", got)
	}
	if got[1].URL != "https://cdn/img-1.png" || got[1].Layout.Width != 800 || got[1].Layout.Thumbhash != "abc" || got[1].MimeType != "image/png" {
		t.Fatalf("image = %+v", got[1])
	}
	if empty := AttachedImages(nil, nil, chat); empty == nil {
		t.Fatal("no images must be an empty list, never null")
	}
}
