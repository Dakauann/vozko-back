package facebook

import (
	"context"
	"net/http"
	"strings"
	"testing"
	"time"

	fbdomain "vozko/domain/facebook"
)

func TestVideoGoesToTheVideoHostWithItsFileURL(t *testing.T) {
	var host string
	svc, calls := postServiceWith(t, func(r *http.Request) string {
		host = r.Host
		return `{"id":"V1"}`
	})
	at := time.Unix(1790000000, 0).UTC()
	id, err := svc.CreateVideo(context.Background(), "PAGE", "tok", fbdomain.VideoInput{FileURL: "https://r2/v.mp4", Title: "Lançamento", Description: "Veja", ScheduledAt: &at})
	if err != nil || id != "V1" {
		t.Fatalf("id %q %v", id, err)
	}
	call := (*calls)[0]
	if call.path != "/v25.0/PAGE/videos" || !strings.HasPrefix(host, "graph-video.facebook.com") {
		t.Fatalf("path %s host %s", call.path, host)
	}
	if call.body["file_url"] != "https://r2/v.mp4" || call.body["title"] != "Lançamento" || call.body["published"] != false || call.body["scheduled_publish_time"] != float64(1790000000) {
		t.Fatalf("body = %v", call.body)
	}
}

func TestReelUploadStartsTransfersAndFinishes(t *testing.T) {
	var transferAuth, transferFile string
	svc, calls := postServiceWith(t, func(r *http.Request) string {
		switch {
		case strings.Contains(r.URL.Path, "/video-upload/"):
			transferAuth, transferFile = r.Header.Get("Authorization"), r.Header.Get("file_url")
			return `{"success":true}`
		case r.Method == http.MethodPost && strings.HasSuffix(r.URL.Path, "/video_reels"):
			return `{"video_id":"R1","upload_url":"https://rupload.facebook.com/video-upload/v25.0/R1","success":true,"post_id":"PAGE_R1"}`
		}
		return `{}`
	})
	ctx := context.Background()
	session, err := svc.StartVideoUpload(ctx, "PAGE", "tok", fbdomain.VideoForReel)
	if err != nil || session.VideoID != "R1" || session.UploadURL == "" {
		t.Fatalf("session %+v %v", session, err)
	}
	if err := svc.TransferVideo(ctx, "tok", *session, "https://r2/r.mp4"); err != nil {
		t.Fatal(err)
	}
	if transferAuth != "OAuth tok" || transferFile != "https://r2/r.mp4" {
		t.Fatalf("transfer headers %q %q", transferAuth, transferFile)
	}
	postID, err := svc.FinishReel(ctx, "PAGE", "tok", "R1", fbdomain.ReelFinish{Description: "novidade"})
	if err != nil || postID != "PAGE_R1" {
		t.Fatalf("finish %q %v", postID, err)
	}
	start, finish := (*calls)[0].body, (*calls)[2].body
	if start["upload_phase"] != "start" || finish["upload_phase"] != "finish" || finish["video_id"] != "R1" || finish["video_state"] != "PUBLISHED" || finish["description"] != "novidade" {
		t.Fatalf("start %v finish %v", start, finish)
	}
}

func TestScheduledReelFinishesAsScheduled(t *testing.T) {
	svc, calls := postServiceWith(t, func(*http.Request) string { return `{"success":true}` })
	at := time.Unix(1790000000, 0).UTC()
	if _, err := svc.FinishReel(context.Background(), "PAGE", "tok", "R1", fbdomain.ReelFinish{ScheduledAt: &at}); err != nil {
		t.Fatal(err)
	}
	if body := (*calls)[0].body; body["video_state"] != "SCHEDULED" || body["scheduled_publish_time"] != float64(1790000000) {
		t.Fatalf("body = %v", body)
	}
}

func TestStoriesAndVideoStatus(t *testing.T) {
	svc, calls := postServiceWith(t, func(r *http.Request) string {
		switch {
		case strings.HasSuffix(r.URL.Path, "/photo_stories"), strings.HasSuffix(r.URL.Path, "/video_stories"):
			return `{"success":true,"post_id":"PAGE_S1"}`
		case strings.HasSuffix(r.URL.Path, "/stories"):
			return `{"data":[{"post_id":"PAGE_S1","status":"PUBLISHED","creation_time":"1790000000","media_type":"photo","media_id":"M1","url":"https://fb/s"}]}`
		}
		return `{"status":{"video_status":"ready"},"post_id":"PAGE_V1"}`
	})
	ctx := context.Background()
	if id, err := svc.CreatePhotoStory(ctx, "PAGE", "tok", "PH1"); err != nil || id != "PAGE_S1" {
		t.Fatalf("photo story %q %v", id, err)
	}
	if id, err := svc.FinishVideoStory(ctx, "PAGE", "tok", "V9"); err != nil || id != "PAGE_S1" {
		t.Fatalf("video story %q %v", id, err)
	}
	stories, err := svc.ListStories(ctx, "PAGE", "tok")
	if err != nil || len(stories) != 1 || stories[0].Status != "PUBLISHED" || stories[0].CreatedTime == nil || stories[0].CreatedTime.Unix() != 1790000000 {
		t.Fatalf("stories %+v %v", stories, err)
	}
	status, err := svc.VideoStatus(ctx, "tok", "V1")
	if err != nil || !status.Ready() || status.PostID != "PAGE_V1" {
		t.Fatalf("status %+v %v", status, err)
	}
	if (*calls)[0].body["photo_id"] != "PH1" || (*calls)[1].body["video_id"] != "V9" || (*calls)[1].body["upload_phase"] != "finish" {
		t.Fatalf("bodies %v %v", (*calls)[0].body, (*calls)[1].body)
	}
}

func TestReelsListAsPostsOnTheirPage(t *testing.T) {
	svc, calls := postServiceWith(t, func(*http.Request) string {
		return `{"data":[{"id":"R1","description":"novidade","updated_time":"2026-09-28T12:00:00+0000","permalink_url":"https://fb/r"}]}`
	})
	out, err := svc.List(context.Background(), "PAGE", "tok", fbdomain.ListReels, 10, "")
	if err != nil || len(out.Items) != 1 {
		t.Fatalf("out %+v %v", out, err)
	}
	reel := out.Items[0]
	if (*calls)[0].path != "/v25.0/PAGE/video_reels" || reel.FBPostID != "PAGE_R1" || reel.Message != "novidade" || reel.KindFor("PAGE") != fbdomain.PostReel {
		t.Fatalf("reel %+v path %s", reel, (*calls)[0].path)
	}
}
