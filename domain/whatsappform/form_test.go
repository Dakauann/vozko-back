package whatsappform

import (
	"errors"
	"strings"
	"testing"
	"time"

	"vozko/domain/shared"
)

func TestFormNames(t *testing.T) {
	if got, err := NormalizeName("  Captação   de leads "); err != nil || got != "Captação de leads" {
		t.Fatalf("NormalizeName = %q, %v", got, err)
	}
	if _, err := NormalizeName(" "); !errors.Is(err, ErrNameRequired) {
		t.Fatalf("err = %v", err)
	}
	if _, err := NormalizeName(strings.Repeat("a", MaxNameLength+1)); !errors.Is(err, ErrNameTooLong) {
		t.Fatalf("err = %v", err)
	}
}

func TestAnArchivedFormRefusesChanges(t *testing.T) {
	at := time.Now()
	if err := (&Form{ArchivedAt: &at}).EnsureActive(); !errors.Is(err, ErrFormArchived) {
		t.Fatalf("err = %v", err)
	}
	if err := (&Form{}).EnsureActive(); err != nil {
		t.Fatal(err)
	}
}

func TestResponseQueryBounds(t *testing.T) {
	q := ResponseQuery{WorkspaceID: "ws", FormID: "f", Scope: seesAll}
	if err := q.Normalize(); err != nil || q.Limit != DefaultPageSize {
		t.Fatalf("default page: %+v %v", q, err)
	}
	from, to := time.Date(2026, 10, 2, 0, 0, 0, 0, time.UTC), time.Date(2026, 10, 1, 0, 0, 0, 0, time.UTC)
	cases := []ResponseQuery{
		{WorkspaceID: "ws", FormID: "f", Scope: seesAll, Limit: MaxPageSize + 1},
		{WorkspaceID: "ws", FormID: "f", Scope: seesAll, Limit: -1},
		{WorkspaceID: "ws", FormID: "f", Scope: seesAll, From: &from, To: &to},
		{WorkspaceID: "ws", FormID: "f", Scope: seesAll, Search: strings.Repeat("a", MaxSearchLength+1)},
		{WorkspaceID: "ws", FormID: "f", Scope: seesAll, Origins: []SendOrigin{"fax"}},
		{WorkspaceID: "ws", FormID: "f", Scope: seesAll, Match: "maybe"},
		{WorkspaceID: "", FormID: "f", Scope: seesAll},
	}
	for _, c := range cases {
		if err := c.Normalize(); !errors.Is(err, ErrListQueryInvalid) && !errors.Is(err, ErrWorkspaceRequired) {
			t.Errorf("%+v must be refused, got %v", c, err)
		}
	}
}

var seesAll = ReadScope{Resolved: true}

func TestAQueryWithoutAResolvedScopeIsRefused(t *testing.T) {
	q := ResponseQuery{WorkspaceID: "ws", FormID: "f"}
	if err := q.Normalize(); !errors.Is(err, ErrForbidden) {
		t.Fatalf("a query that never resolved who is reading must be refused, got %v", err)
	}
}

func TestResponseQueryReadsTheSharedKeysetCursor(t *testing.T) {
	at := time.Date(2026, 10, 19, 12, 30, 0, 123000000, time.UTC)
	q := ResponseQuery{WorkspaceID: "ws", FormID: "f", Scope: seesAll, After: shared.Keyset{At: at, ID: "resp-9"}.Encode()}
	if err := q.Normalize(); err != nil {
		t.Fatal(err)
	}
	key, paged := q.Cursor()
	if !paged || !key.At.Equal(at) || key.ID != "resp-9" {
		t.Fatalf("cursor = %+v %v", key, paged)
	}
	bad := ResponseQuery{WorkspaceID: "ws", FormID: "f", Scope: seesAll, After: "%%%"}
	if err := bad.Normalize(); !errors.Is(err, ErrListQueryInvalid) {
		t.Fatalf("a broken cursor is refused, got %v", err)
	}
	if _, paged := (ResponseQuery{}).Cursor(); paged {
		t.Fatal("no cursor is the first page")
	}
}

func TestDescriptionsAreBounded(t *testing.T) {
	if got, err := NormalizeDescription("  Para eventos  "); err != nil || got != "Para eventos" {
		t.Fatalf("NormalizeDescription = %q %v", got, err)
	}
	if _, err := NormalizeDescription(strings.Repeat("a", MaxDescriptionLength+1)); !errors.Is(err, ErrDescriptionTooLong) || !IsInputRefusal(err) {
		t.Fatalf("err = %v", err)
	}
}

func TestMediaIDsListsEveryImageOnce(t *testing.T) {
	doc := oneScreen(
		fieldBlock(textField("f_name", "Nome")),
		Block{Key: "b_i1", Display: &Display{Kind: DisplayImage, MediaID: "m1"}},
		Block{Key: "b_i2", Display: &Display{Kind: DisplayImage, MediaID: "m1"}},
		Block{Key: "b_i3", Display: &Display{Kind: DisplayImage, MediaID: "m2"}},
	)
	if got := doc.MediaIDs(); len(got) != 2 || got[0] != "m1" || got[1] != "m2" {
		t.Fatalf("MediaIDs = %v", got)
	}
}

func TestMissingImagesAreBuilderProblems(t *testing.T) {
	doc := oneScreen(fieldBlock(textField("f_name", "Nome")), Block{Key: "b_i", Display: &Display{Kind: DisplayImage, MediaID: "gone"}})
	problems := MissingAssets(doc, Assets{})
	if len(problems) != 1 || problems[0].Block != "b_i" || problems[0].Property != "mediaId" || problems[0].Code != ProblemUnknownTarget {
		t.Fatalf("problems = %+v", problems)
	}
	if len(MissingAssets(doc, Assets{"gone": {1}})) != 0 {
		t.Fatal("a loaded image is not a problem")
	}
}

func TestCloneDocumentIsDeep(t *testing.T) {
	doc := twoScreens()
	copied := CloneDocument(doc)
	copied.Screens[0].Blocks[0].Field.Label = "Outro"
	if doc.Screens[0].Blocks[0].Field.Label != "Nome" {
		t.Fatal("a clone shares nothing with its source")
	}
}

func TestAMetaRefusalHasItsOwnCode(t *testing.T) {
	err := &MetaRejection{Code: 139000, Message: "Blocked"}
	if ErrorCode(err) != "whatsapp_form_meta_rejected" {
		t.Fatalf("code = %q", ErrorCode(err))
	}
}
