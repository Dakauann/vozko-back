package instagram_repository

import (
	"testing"

	"vozko/infra/database/schema"
)

func TestDedupeCommentRecordsKeepsLastState(t *testing.T) {
	first := &schema.InstagramComment{IGCommentID: "comment-1", Text: "old"}
	second := &schema.InstagramComment{IGCommentID: "comment-2", Text: "other"}
	latest := &schema.InstagramComment{IGCommentID: "comment-1", Text: "latest"}

	got := dedupeCommentRecords([]*schema.InstagramComment{first, second, latest})
	if len(got) != 2 {
		t.Fatalf("got %d records, want 2", len(got))
	}
	if got[0] != latest || got[0].Text != "latest" {
		t.Fatalf("duplicate was not replaced by its latest state: %+v", got[0])
	}
	if got[1] != second {
		t.Fatalf("unique record moved or changed: %+v", got[1])
	}
}
