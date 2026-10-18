package notification

import (
	"bytes"
	"errors"
	"strings"
	"testing"
	"time"

	"vozko/domain/shared"
)

func TestListQueryNormalizesLimitAndCategory(t *testing.T) {
	cases := []struct {
		in   ListQuery
		want int
	}{
		{ListQuery{}, DefaultPageSize},
		{ListQuery{Limit: -3}, DefaultPageSize},
		{ListQuery{Limit: 7}, 7},
		{ListQuery{Limit: MaxPageSize + 50}, MaxPageSize},
	}
	for _, tc := range cases {
		if got := tc.in.Normalize().Limit; got != tc.want {
			t.Fatalf("Normalize(%+v).Limit = %d, want %d", tc.in, got, tc.want)
		}
	}
}

func TestParseListQueryRefusesUnknownCategoryAndForeignCursor(t *testing.T) {
	if _, err := ParseListQuery("misc", false, "", 0); !errors.Is(err, ErrCategoryUnknown) {
		t.Fatalf("unknown category filter = %v, want ErrCategoryUnknown", err)
	}
	if _, err := ParseListQuery("", false, "not-a-cursor!", 0); !errors.Is(err, ErrCursorInvalid) {
		t.Fatalf("foreign cursor = %v, want ErrCursorInvalid", err)
	}
	cursor := shared.Keyset{At: time.Date(2026, 10, 16, 12, 0, 0, 0, time.UTC), ID: "n-9"}.Encode()
	q, err := ParseListQuery("billing", true, cursor, 5)
	if err != nil {
		t.Fatalf("parse: %v", err)
	}
	if q.Category != CategoryBilling || !q.UnreadOnly || !q.Paged || q.Before.ID != "n-9" || q.Limit != 5 {
		t.Fatalf("parsed %+v", q)
	}
}

func TestPageOfTrimsTheProbeRowAndPointsAtTheLastShownItem(t *testing.T) {
	at := time.Date(2026, 10, 16, 12, 0, 0, 0, time.UTC)
	items := []Item{{ID: "a", CreatedAt: at}, {ID: "b", CreatedAt: at.Add(-time.Minute)}, {ID: "c", CreatedAt: at.Add(-2 * time.Minute)}}

	page := PageOf(items, 2)
	if len(page.Items) != 2 || page.Next == "" {
		t.Fatalf("a probe row beyond the limit means another page: %+v", page)
	}
	next, _, _ := shared.ParseKeyset(page.Next)
	if next.ID != "b" || !next.At.Equal(items[1].CreatedAt) {
		t.Fatalf("next cursor = %+v, want the last shown item", next)
	}
	if last := PageOf(items, 3); last.Next != "" || len(last.Items) != 3 {
		t.Fatalf("no probe row means the last page: %+v", last)
	}
}

func TestReadIDsAreBoundedAndDeduplicated(t *testing.T) {
	if _, err := ReadIDs(nil); !errors.Is(err, ErrIDsInvalid) {
		t.Fatal("an empty id list must be refused")
	}
	if _, err := ReadIDs([]string{" "}); !errors.Is(err, ErrIDsInvalid) {
		t.Fatal("blank ids must be refused")
	}
	tooMany := make([]string, MaxIDsPerChange+1)
	for i := range tooMany {
		tooMany[i] = strings.Repeat("x", i%7+1)
	}
	if _, err := ReadIDs(tooMany); !errors.Is(err, ErrIDsInvalid) {
		t.Fatal("more than the cap must be refused")
	}
	got, err := ReadIDs([]string{" a ", "b", "a"})
	if err != nil || len(got) != 2 || got[0] != "a" || got[1] != "b" {
		t.Fatalf("ReadIDs = %v, %v", got, err)
	}
}

func TestPreferencesMuteEmailButCriticalAlwaysArrives(t *testing.T) {
	p := Preferences{EmailMuted: []Category{CategoryCampaigns}}
	if p.WantsEmail(CategoryCampaigns, SeverityWarning) {
		t.Fatal("a muted category must not email")
	}
	if !p.WantsEmail(CategoryCampaigns, SeverityCritical) {
		t.Fatal("critical notifications always email")
	}
	if !p.WantsEmail(CategoryBilling, SeverityInfo) {
		t.Fatal("categories that are not muted email")
	}
}

func TestMutedCategoriesAreValidatedAndDeduplicated(t *testing.T) {
	if _, err := MutedCategories([]string{"campaigns", "misc"}); !errors.Is(err, ErrCategoryUnknown) {
		t.Fatalf("unknown muted category = %v", err)
	}
	got, err := MutedCategories([]string{"billing", "campaigns", "billing"})
	if err != nil || len(got) != 2 || got[0] != CategoryBilling || got[1] != CategoryCampaigns {
		t.Fatalf("MutedCategories = %v, %v", got, err)
	}
}

func TestClassifyImageAcceptsOnlyRealImagesWithinTheLimit(t *testing.T) {
	png := append([]byte("\x89PNG\r\n\x1a\n"), bytes.Repeat([]byte{0}, 32)...)
	img, err := ClassifyImage(png)
	if err != nil || img.MIME != "image/png" || img.Ext != ".png" {
		t.Fatalf("png = %+v, %v", img, err)
	}
	if _, err := ClassifyImage(nil); !errors.Is(err, ErrImageEmpty) {
		t.Fatalf("empty = %v", err)
	}
	if _, err := ClassifyImage([]byte("<svg xmlns='http://www.w3.org/2000/svg'></svg>")); !errors.Is(err, ErrImageType) {
		t.Fatalf("svg must be refused (scripts), got %v", err)
	}
	if _, err := ClassifyImage(append(png, make([]byte, MaxImageBytes)...)); !errors.Is(err, ErrImageTooLarge) {
		t.Fatalf("oversized = %v", err)
	}
}

func TestBroadcastValidationAndPerWorkspaceDraft(t *testing.T) {
	b := BroadcastDraft{
		Target:   BroadcastTarget{WorkspaceIDs: []string{"ws-1", " ws-2 ", "ws-1"}},
		Audience: Managers(),
		Category: CategoryAnnouncements,
		Title:    "Novidade",
		Body:     "Chegou o disparo inteligente.",
		Email:    true,
	}.Normalize()
	if err := b.Validate(); err != nil {
		t.Fatalf("valid broadcast refused: %v", err)
	}
	if len(b.Target.WorkspaceIDs) != 2 || b.Severity != SeverityInfo {
		t.Fatalf("normalize = %+v", b)
	}

	broadcast := Broadcast{ID: "b-1", BroadcastDraft: b, CreatedBy: "admin-1"}
	d := broadcast.DraftFor("ws-2")
	if d.WorkspaceID != "ws-2" || d.DedupKey != "broadcast:b-1" || d.BroadcastID != "b-1" || d.Source != SourceBroadcast || !d.Email || d.CreatedBy != "admin-1" {
		t.Fatalf("draft = %+v", d)
	}
	if err := d.Validate(); err != nil {
		t.Fatalf("the per-workspace draft must be valid: %v", err)
	}

	for name, target := range map[string]BroadcastTarget{
		"nothing":  {},
		"both":     {All: true, WorkspaceIDs: []string{"ws-1"}},
		"too many": {WorkspaceIDs: make([]string, MaxBroadcastWorkspaces+1)},
	} {
		bad := b
		bad.Target = target
		if err := bad.Normalize().Validate(); !errors.Is(err, ErrTargetInvalid) {
			t.Fatalf("%s target = %v, want ErrTargetInvalid", name, err)
		}
	}
}

func TestEmailDedupKeyIsSharedAcrossWorkspacesOfOneBroadcast(t *testing.T) {
	direct := Record{ID: "n-1"}
	fromBroadcast := Record{ID: "n-2", Draft: Draft{BroadcastID: "b-1"}}
	if direct.EmailDedupKey("u-1") != "notification:n-1:u-1" {
		t.Fatalf("direct = %q", direct.EmailDedupKey("u-1"))
	}
	if fromBroadcast.EmailDedupKey("u-1") != "notification:broadcast:b-1:u-1" {
		t.Fatalf("broadcast = %q", fromBroadcast.EmailDedupKey("u-1"))
	}
}

func TestErrorCodesCoverEveryRefusal(t *testing.T) {
	for _, err := range []error{
		ErrWorkspaceRequired, ErrCategoryUnknown, ErrSeverityUnknown, ErrTitleRequired, ErrTitleTooLong,
		ErrBodyTooLong, ErrActionURLInvalid, ErrActionLabelTooLong, ErrImageForeign, ErrAudienceInvalid,
		ErrIDsInvalid, ErrImageEmpty, ErrImageTooLarge, ErrImageType, ErrTargetInvalid, ErrCursorInvalid,
		ErrViewerRequired, ErrNotFound, ErrReadStatusUnknown,
	} {
		if ErrorCode(err) == "" {
			t.Fatalf("%v has no error code", err)
		}
	}
	if ErrorCode(errors.New("boom")) != "" {
		t.Fatal("unknown errors have no code")
	}
}
