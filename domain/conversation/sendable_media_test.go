package conversation

import (
	"errors"
	"testing"

	"vozko/domain/media"
	"vozko/domain/shared"
)

type entryMediaRows map[string]*ConversationMedia

func (r entryMediaRows) GetInEntry(entryID string, entryType shared.EntryType, id string) (*ConversationMedia, error) {
	row, ok := r[id]
	if !ok || row.EntryID != entryID || row.EntryType != entryType {
		return nil, ErrMediaNotFound
	}
	return row, nil
}

type failingEntryMedia struct{}

func (failingEntryMedia) GetInEntry(string, shared.EntryType, string) (*ConversationMedia, error) {
	return nil, errors.New("db down")
}

type looseEntryMedia struct{ row *ConversationMedia }

func (l looseEntryMedia) GetInEntry(string, shared.EntryType, string) (*ConversationMedia, error) {
	return l.row, nil
}

type libraryRows map[string]*media.Media

func (r libraryRows) GetMediaForWorkspace(workspaceID, id string) (*media.Media, error) {
	row, ok := r[id]
	if !ok || row.WorkspaceID != workspaceID {
		return nil, media.ErrMediaNotFound
	}
	return row, nil
}

type looseLibrary struct{ row *media.Media }

func (l looseLibrary) GetMediaForWorkspace(string, string) (*media.Media, error) { return l.row, nil }

func uploadedPhoto(id, entryID string, purpose MediaPurpose) *ConversationMedia {
	return &ConversationMedia{ID: id, EntryID: entryID, EntryType: shared.EntryTypeWhatsApp, Type: MediaTypeImage, URL: "https://cdn/" + id + ".jpg", Purpose: purpose}
}

func assertMediaRefused(t *testing.T, err error) {
	t.Helper()
	if !errors.Is(err, ErrMediaNotSendable) {
		t.Fatalf("err = %v, want ErrMediaNotSendable", err)
	}
	if code := SendFailureCode(err, "send_failed"); code != MediaNotSendableCode {
		t.Fatalf("code = %q, want %q", code, MediaNotSendableCode)
	}
}

func TestAnUploadOfThisConversationIsSendable(t *testing.T) {
	rows := entryMediaRows{"m-1": uploadedPhoto("m-1", "entry-1", MediaPurposeMessage)}

	got, err := SendableUpload(rows, "entry-1", shared.EntryTypeWhatsApp, " m-1 ")
	if err != nil {
		t.Fatalf("SendableUpload: %v", err)
	}
	if got.ID != "m-1" {
		t.Fatalf("got %q, want m-1", got.ID)
	}
}

func TestAnUploadOfAnotherConversationIsRefused(t *testing.T) {
	rows := entryMediaRows{"m-1": uploadedPhoto("m-1", "entry-of-another-workspace", MediaPurposeMessage)}

	_, err := SendableUpload(rows, "entry-1", shared.EntryTypeWhatsApp, "m-1")
	assertMediaRefused(t, err)
}

func TestARowTheReaderLetThroughIsStillCheckedAgainstTheEntry(t *testing.T) {
	_, err := SendableUpload(looseEntryMedia{row: uploadedPhoto("m-1", "entry-2", MediaPurposeMessage)}, "entry-1", shared.EntryTypeWhatsApp, "m-1")
	assertMediaRefused(t, err)
}

func TestANoteImageIsNeverSendable(t *testing.T) {
	rows := entryMediaRows{"note-1": uploadedPhoto("note-1", "entry-1", MediaPurposeNote)}

	_, err := SendableUpload(rows, "entry-1", shared.EntryTypeWhatsApp, "note-1")
	assertMediaRefused(t, err)
}

func TestAnUploadIsRefusedWhenAnythingIsUnknown(t *testing.T) {
	cases := map[string]struct {
		reader  EntryMediaReader
		entryID string
		id      string
	}{
		"lookup error":   {failingEntryMedia{}, "entry-1", "m-1"},
		"no reader":      {nil, "entry-1", "m-1"},
		"missing row":    {entryMediaRows{}, "entry-1", "m-1"},
		"nil row":        {looseEntryMedia{}, "entry-1", "m-1"},
		"no media id":    {entryMediaRows{"m-1": uploadedPhoto("m-1", "entry-1", MediaPurposeMessage)}, "entry-1", " "},
		"no entry":       {looseEntryMedia{row: uploadedPhoto("m-1", "", MediaPurposeMessage)}, "", "m-1"},
		"unknown kind":   {looseEntryMedia{row: uploadedPhoto("m-1", "entry-1", "mystery")}, "entry-1", "m-1"},
		"empty purpose":  {looseEntryMedia{row: uploadedPhoto("m-1", "entry-1", "")}, "entry-1", "m-1"},
		"no url on file": {looseEntryMedia{row: &ConversationMedia{ID: "m-1", EntryID: "entry-1", EntryType: shared.EntryTypeWhatsApp, Purpose: MediaPurposeMessage}}, "entry-1", "m-1"},
	}
	for name, c := range cases {
		t.Run(name, func(t *testing.T) {
			_, err := SendableUpload(c.reader, c.entryID, shared.EntryTypeWhatsApp, c.id)
			assertMediaRefused(t, err)
		})
	}
}

func TestLibraryMediaOfTheWorkspaceIsSendable(t *testing.T) {
	rows := libraryRows{"lib-1": {ID: "lib-1", WorkspaceID: "ws-1", URL: "https://cdn/promo.mp4", Type: media.MediaTypeProductVideo}}

	got, err := SendableLibraryMedia(rows, "ws-1", "lib-1")
	if err != nil {
		t.Fatalf("SendableLibraryMedia: %v", err)
	}
	if got.ID != "lib-1" {
		t.Fatalf("got %q, want lib-1", got.ID)
	}
}

func TestLibraryMediaIsRefusedOutsideItsWorkspaceOrWhenUnknown(t *testing.T) {
	foreign := &media.Media{ID: "lib-1", WorkspaceID: "ws-2", URL: "https://cdn/promo.mp4", Type: media.MediaTypeProductVideo}
	cases := map[string]struct {
		reader    LibraryMediaReader
		workspace string
	}{
		"another workspace":           {libraryRows{"lib-1": foreign}, "ws-1"},
		"reader let another one pass": {looseLibrary{row: foreign}, "ws-1"},
		"no workspace":                {looseLibrary{row: foreign}, ""},
		"no reader":                   {nil, "ws-2"},
		"nil row":                     {looseLibrary{}, "ws-2"},
		"private kind":                {looseLibrary{row: &media.Media{ID: "lib-1", WorkspaceID: "ws-2", URL: "https://cdn/l.csv", Type: media.MediaTypeLeadImport}}, "ws-2"},
	}
	for name, c := range cases {
		t.Run(name, func(t *testing.T) {
			_, err := SendableLibraryMedia(c.reader, c.workspace, "lib-1")
			assertMediaRefused(t, err)
		})
	}
}

func TestARefusalLooksLikeAMissingFileSoIdsCannotBeProbed(t *testing.T) {
	_, foreign := SendableLibraryMedia(libraryRows{"lib-1": {ID: "lib-1", WorkspaceID: "ws-2", URL: "https://cdn/a", Type: media.MediaTypeProductImage}}, "ws-1", "lib-1")
	_, missing := SendableLibraryMedia(libraryRows{}, "ws-1", "lib-1")
	if foreign.Error() != missing.Error() {
		t.Fatalf("foreign %q and missing %q read differently", foreign, missing)
	}
	if !errors.Is(foreign, media.ErrMediaNotFound) {
		t.Fatal("handlers that answer a missing library file with 404 must keep answering a refusal the same way")
	}
}

func TestStoredMediaDefaultsToMessagePurposeAndRejectsUnknownOnes(t *testing.T) {
	row := uploadedPhoto("m-1", "entry-1", "")
	row.Normalize()
	if row.Purpose != MediaPurposeMessage {
		t.Fatalf("purpose = %q, want message", row.Purpose)
	}
	row.Purpose = "mystery"
	if err := row.Validate(); !errors.Is(err, ErrMediaPurposeInvalid) {
		t.Fatalf("err = %v, want ErrMediaPurposeInvalid", err)
	}
	row.Purpose = MediaPurposeNote
	if err := row.Validate(); err != nil {
		t.Fatalf("a note image must be storable: %v", err)
	}
}

func TestANilReaderIsRefused(t *testing.T) {
	_, err := SendableUpload(nil, "entry-1", shared.EntryTypeEmail, "m-1")
	assertMediaRefused(t, err)

	_, err = SendableLibraryMedia(nil, "ws-1", "lib-1")
	assertMediaRefused(t, err)
}
