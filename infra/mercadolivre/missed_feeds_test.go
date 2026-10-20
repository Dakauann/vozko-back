package mercadolivre

import (
	"context"
	"errors"
	"testing"

	mldomain "vozko/domain/mercadolivre"
)

func TestMissedFeedsFlowThroughTheNotificationNormalizer(t *testing.T) {
	fs := newFakeServer(t, jsonReply(200, fixture(t, "missed_feeds_doc_example_fixed.json")))
	limiters := newFakeLimiters()
	s, err := NewMissedFeedService(newTestClient(t, fs, limiters))
	if err != nil {
		t.Fatal(err)
	}
	got, err := s.MissedFeeds(context.Background(), sellerCreds, mldomain.MissedFeedQuery{AppID: "3486171129139063", Topic: mldomain.TopicMessages, Offset: 10, Limit: 500})
	if err != nil {
		t.Fatal(err)
	}
	r := fs.last()
	if r.Path != "/missed_feeds" || r.Query.Get("app_id") != "3486171129139063" || r.Query.Get("topic") != "messages" ||
		r.Query.Get("offset") != "10" || r.Query.Get("limit") != "100" {
		t.Fatalf("%s %v", r.Path, r.Query)
	}
	assertBearer(t, r)
	if len(got) != 2 || got[0].ID != "5da8a1b24be30a49eb66c52a" || got[0].Resource != "a35cf79864a845ca9a3bf6aee59bb4d7" ||
		got[0].UserID != "465432224" || got[1].Topic != "items" {
		t.Fatalf("notifications = %+v", got)
	}
	job, err := mldomain.Normalize(got[0], "3486171129139063")
	if err != nil || job.Kind != mldomain.JobFetchMessage {
		t.Fatalf("Normalize = %+v, %v", job, err)
	}
	if _, err := mldomain.Normalize(got[1], "3486171129139063"); !errors.Is(err, mldomain.ErrInvalidResource) {
		t.Fatalf("an item of another site must be refused: %v", err)
	}
}

func TestMissedFeedsRefusals(t *testing.T) {
	fs := newFakeServer(t, jsonReply(200, `{"messages":[]}`))
	s, _ := NewMissedFeedService(newTestClient(t, fs, newFakeLimiters()))
	cases := []mldomain.MissedFeedQuery{
		{},
		{AppID: "abc"},
		{AppID: "1", Topic: "payments"},
		{AppID: "1", Topic: mldomain.TopicItems},
	}
	for i, q := range cases {
		if _, err := s.MissedFeeds(context.Background(), sellerCreds, q); err == nil {
			t.Errorf("query %d accepted", i)
		}
	}
	if fs.count() != 0 {
		t.Fatal("refusals must not reach Mercado Livre")
	}
	if _, err := s.MissedFeeds(context.Background(), sellerCreds, mldomain.MissedFeedQuery{AppID: "1", Topic: mldomain.TopicItems, SiteID: "MLB"}); err != nil {
		t.Fatal(err)
	}
	if fs.last().Query.Get("site_id") != "MLB" {
		t.Fatalf("items need the site: %v", fs.last().Query)
	}
}

func TestMissedFeedsRefuseAnUndecodableEntry(t *testing.T) {
	fs := newFakeServer(t, jsonReply(200, `{"messages":[{"user_id":"12a"}]}`))
	s, _ := NewMissedFeedService(newTestClient(t, fs, newFakeLimiters()))
	if _, err := s.MissedFeeds(context.Background(), sellerCreds, mldomain.MissedFeedQuery{AppID: "1"}); !errors.Is(err, mldomain.ErrInvalidNotification) {
		t.Fatalf("err = %v", err)
	}
}
