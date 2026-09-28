package facebook

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"strings"
	"testing"
	"time"

	fbdomain "vozko/domain/facebook"
	"vozko/infra/meta/metatest"
)

type recordedCall struct {
	method string
	path   string
	query  map[string]string
	body   map[string]any
}

func postServiceWith(t *testing.T, respond func(r *http.Request) string) (fbdomain.PostService, *[]recordedCall) {
	t.Helper()
	calls := &[]recordedCall{}
	svc, err := NewPostService(GraphConfig{HTTPClient: metatest.Server(t, func(w http.ResponseWriter, r *http.Request) {
		call := recordedCall{method: r.Method, path: r.URL.Path, query: map[string]string{}}
		for k := range r.URL.Query() {
			call.query[k] = r.URL.Query().Get(k)
		}
		if raw, _ := io.ReadAll(r.Body); len(raw) > 0 {
			_ = json.Unmarshal(raw, &call.body)
		}
		*calls = append(*calls, call)
		_, _ = w.Write([]byte(respond(r)))
	})})
	if err != nil {
		t.Fatal(err)
	}
	return svc, calls
}

const publishedPage = `{"data":[{"id":"PAGE_1","from":{"id":"PAGE"},"message":"Olá","created_time":"2026-09-28T12:00:00+0000",
 "permalink_url":"https://facebook.com/1","full_picture":"https://scontent/p.jpg","is_published":true,"is_hidden":false,
 "shares":{"count":3},"reactions":{"summary":{"total_count":42}},"comments":{"summary":{"total_count":7}},
 "attachments":{"data":[{"media_type":"photo","type":"photo","media":{"image":{"src":"https://scontent/t.jpg"}}}]}}],
 "paging":{"cursors":{"after":"CUR"},"next":"https://graph.facebook.com/next"}}`

func TestListPublishedDecodesPostsAndTheNextCursor(t *testing.T) {
	svc, calls := postServiceWith(t, func(*http.Request) string { return publishedPage })

	out, err := svc.List(context.Background(), "PAGE", "tok", fbdomain.ListPublished, 25, "PREV")
	if err != nil {
		t.Fatal(err)
	}
	call := (*calls)[0]
	if call.path != "/v25.0/PAGE/published_posts" || call.query["after"] != "PREV" || call.query["limit"] != "25" || !strings.Contains(call.query["fields"], "attachments") {
		t.Fatalf("call = %+v", call)
	}
	if !out.HasNext || out.NextCursor != "CUR" || len(out.Items) != 1 {
		t.Fatalf("page = %+v", out)
	}
	post := out.Items[0]
	if post.FBPostID != "PAGE_1" || post.FromID != "PAGE" || post.ReactionsCount != 42 || post.CommentsCount != 7 || post.SharesCount != 3 {
		t.Fatalf("post = %+v", post)
	}
	if post.CreatedTime == nil || !post.CreatedTime.Equal(time.Date(2026, 9, 28, 12, 0, 0, 0, time.UTC)) {
		t.Fatalf("created = %v", post.CreatedTime)
	}
	if post.KindFor("PAGE") != fbdomain.PostPhoto || post.AssetURL(true) != "https://scontent/t.jpg" || post.AssetURL(false) != "https://scontent/p.jpg" {
		t.Fatalf("attachments = %+v", post.Attachments)
	}
}

func TestLastPageHasNoNextCursor(t *testing.T) {
	svc, calls := postServiceWith(t, func(*http.Request) string {
		return `{"data":[{"id":"PAGE_2","scheduled_publish_time":1790000000}],"paging":{"cursors":{"after":"X"}}}`
	})
	out, err := svc.List(context.Background(), "PAGE", "tok", fbdomain.ListScheduled, 10, "")
	if err != nil {
		t.Fatal(err)
	}
	if (*calls)[0].path != "/v25.0/PAGE/scheduled_posts" || out.HasNext || out.NextCursor != "" {
		t.Fatalf("path %s, page %+v", (*calls)[0].path, out)
	}
	if at := out.Items[0].ScheduledPublishTime; at == nil || at.Unix() != 1790000000 {
		t.Fatalf("scheduled = %v", at)
	}
}

func TestScheduledAlbumBody(t *testing.T) {
	svc, calls := postServiceWith(t, func(*http.Request) string { return `{"id":"PAGE_9"}` })
	at := time.Unix(1790000000, 0).UTC()

	id, err := svc.CreateFeedPost(context.Background(), "PAGE", "tok", fbdomain.FeedPostInput{
		Message: "Álbum", AttachedMedia: []string{"p1", "p2"}, ScheduledAt: &at,
	})
	if err != nil || id != "PAGE_9" {
		t.Fatalf("id %q, %v", id, err)
	}
	body := (*calls)[0].body
	if (*calls)[0].path != "/v25.0/PAGE/feed" || body["published"] != false || body["scheduled_publish_time"] != float64(1790000000) || body["unpublished_content_type"] != "SCHEDULED" {
		t.Fatalf("body = %v", body)
	}
	media, _ := body["attached_media"].([]any)
	if len(media) != 2 || media[0].(map[string]any)["media_fbid"] != "p1" {
		t.Fatalf("attached = %v", body["attached_media"])
	}
}

func TestImmediateLinkPostCarriesNoScheduleFields(t *testing.T) {
	svc, calls := postServiceWith(t, func(*http.Request) string { return `{"id":"PAGE_1"}` })
	if _, err := svc.CreateFeedPost(context.Background(), "PAGE", "tok", fbdomain.FeedPostInput{Link: "https://loja.example"}); err != nil {
		t.Fatal(err)
	}
	body := (*calls)[0].body
	if _, ok := body["published"]; ok || body["link"] != "https://loja.example" {
		t.Fatalf("body = %v", body)
	}
}

func TestUploadPhotoReturnsBothIDs(t *testing.T) {
	svc, calls := postServiceWith(t, func(*http.Request) string { return `{"id":"PH","post_id":"PAGE_PH"}` })
	out, err := svc.UploadPhoto(context.Background(), "PAGE", "tok", fbdomain.PhotoInput{URL: "https://r2/a.jpg", Caption: "c", Published: true})
	if err != nil || out.PhotoID != "PH" || out.PostID != "PAGE_PH" {
		t.Fatalf("out %+v, %v", out, err)
	}
	if body := (*calls)[0].body; body["url"] != "https://r2/a.jpg" || body["published"] != true || body["caption"] != "c" {
		t.Fatalf("body = %v", body)
	}
}

func TestUpdateSendsOnlyTheChangedFields(t *testing.T) {
	svc, calls := postServiceWith(t, func(*http.Request) string { return `{"success":true}` })
	hidden := true
	if err := svc.Update(context.Background(), "tok", "PAGE_1", fbdomain.PostUpdate{IsHidden: &hidden}); err != nil {
		t.Fatal(err)
	}
	if err := svc.Update(context.Background(), "tok", "PAGE_1", fbdomain.PostUpdate{PublishNow: true}); err != nil {
		t.Fatal(err)
	}
	if b := (*calls)[0].body; len(b) != 1 || b["is_hidden"] != true {
		t.Fatalf("hide body = %v", b)
	}
	if b := (*calls)[1].body; len(b) != 1 || b["is_published"] != true {
		t.Fatalf("publish body = %v", b)
	}
}

func TestUnacknowledgedDeleteIsAnError(t *testing.T) {
	svc, calls := postServiceWith(t, func(*http.Request) string { return `{"success":false}` })
	if err := svc.Delete(context.Background(), "tok", "PAGE_1"); err == nil {
		t.Fatal("expected an error")
	}
	if (*calls)[0].method != http.MethodDelete {
		t.Fatalf("method = %s", (*calls)[0].method)
	}
}
