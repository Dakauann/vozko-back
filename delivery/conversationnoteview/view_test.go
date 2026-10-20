package conversationnoteview

import (
	"encoding/json"
	"sort"
	"strings"
	"testing"
	"time"

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
	if strings.Join(keys, ",") != "author_id,author_name,body,created_at,entry_id,entry_type,id,mentions" {
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
