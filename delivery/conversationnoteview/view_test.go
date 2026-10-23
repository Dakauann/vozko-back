package conversationnoteview

import (
	"encoding/json"
	"sort"
	"strings"
	"testing"
	"time"

	"vozko/domain/conversation"
	"vozko/domain/conversationnote"
	"vozko/domain/shared"
)

func TestTheNoteWireShapeIsTheContractShape(t *testing.T) {
	gone := time.Date(2026, 10, 18, 13, 0, 0, 0, time.UTC)
	note := conversationnote.Authored{
		Note: conversationnote.Note{
			ID: "note-1", WorkspaceID: "ws-1", EntryType: shared.EntryTypeWhatsApp, EntryID: "entry-1",
			AuthorID: "u-1", Body: "ligar amanhã", Mentions: []string{"u-2"},
			CreatedAt: time.Date(2026, 10, 18, 12, 0, 0, 0, time.UTC), DeletedAt: &gone,
		},
		AuthorName: "Carla",
	}

	raw, err := json.Marshal(Of(note))
	if err != nil {
		t.Fatal(err)
	}
	var fields map[string]any
	if err := json.Unmarshal(raw, &fields); err != nil {
		t.Fatal(err)
	}
	keys := make([]string, 0, len(fields))
	for k := range fields {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	if strings.Join(keys, ",") != "author_id,author_name,body,created_at,entry_id,entry_type,id,images,mentions" {
		t.Fatalf("keys = %v; the workspace and the deletion stamp never go on the wire", keys)
	}
	if fields["entry_type"] != "whatsapp" || fields["author_name"] != "Carla" || fields["created_at"] != "2026-10-18T12:00:00Z" {
		t.Fatalf("fields = %v", fields)
	}
}

func TestMentionsAreNeverNullOnTheWire(t *testing.T) {
	raw, _ := json.Marshal(Of(conversationnote.Authored{}))
	if !strings.Contains(string(raw), `"mentions":[]`) {
		t.Fatalf("payload = %s, want an empty mentions list", raw)
	}
}

func TestImagesAreNeverNullOnTheWire(t *testing.T) {
	raw, _ := json.Marshal(Of(conversationnote.Authored{}))
	if !strings.Contains(string(raw), `"images":[]`) {
		t.Fatalf("payload = %s, want an empty images list", raw)
	}
}

func TestAnImageCarriesItsURLDimensionsAndThumbhash(t *testing.T) {
	note := conversationnote.Authored{
		Note: conversationnote.Note{ImageIDs: []string{"img-1"}},
		Images: []conversationnote.Image{{
			ID: "img-1", URL: "https://cdn/img-1.webp", MimeType: "image/webp",
			Layout: conversation.MediaLayout{Width: 800, Height: 600, Thumbhash: "hash"},
		}},
	}
	raw, _ := json.Marshal(Of(note))
	want := `"images":[{"id":"img-1","url":"https://cdn/img-1.webp","mime_type":"image/webp","width":800,"height":600,"thumbhash":"hash"}]`
	if !strings.Contains(string(raw), want) {
		t.Fatalf("payload = %s, want %s", raw, want)
	}
	if strings.Contains(string(raw), "image_ids") {
		t.Fatal("the raw id list stays inside; each image already names its id")
	}
}

func TestAnImageWithoutAKnownLayoutLeavesTheSizeOut(t *testing.T) {
	raw, _ := json.Marshal(ImageOf(conversationnote.Image{ID: "img-1", URL: "https://cdn/a.jpg", MimeType: "image/jpeg"}))
	if string(raw) != `{"id":"img-1","url":"https://cdn/a.jpg","mime_type":"image/jpeg"}` {
		t.Fatalf("payload = %s", raw)
	}
}
