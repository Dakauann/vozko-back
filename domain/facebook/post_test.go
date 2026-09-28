package facebook

import (
	"errors"
	"strings"
	"testing"
	"time"
)

var postNow = time.Date(2026, 9, 28, 12, 0, 0, 0, time.UTC)

func image(url string) MediaRef {
	return MediaRef{URL: url, MIMEType: "image/jpeg", SizeBytes: 1 << 20}
}

func TestPublishRequestValidationByKind(t *testing.T) {
	in := func(minutes int) *time.Time {
		at := postNow.Add(time.Duration(minutes) * time.Minute)
		return &at
	}
	cases := []struct {
		name string
		req  PublishRequest
		ok   bool
	}{
		{"text", PublishRequest{Kind: PublishText, Message: "Novidades!"}, true},
		{"empty text", PublishRequest{Kind: PublishText, Message: "  "}, false},
		{"text with media", PublishRequest{Kind: PublishText, Message: "x", Media: []MediaRef{image("https://r2/a.jpg")}}, false},
		{"link", PublishRequest{Kind: PublishLink, Link: "https://loja.example/produto"}, true},
		{"link over http", PublishRequest{Kind: PublishLink, Link: "http://loja.example"}, false},
		{"link missing", PublishRequest{Kind: PublishLink, Message: "x"}, false},
		{"photo", PublishRequest{Kind: PublishPhoto, Media: []MediaRef{image("https://r2/a.jpg")}}, true},
		{"photo too big", PublishRequest{Kind: PublishPhoto, Media: []MediaRef{{URL: "https://r2/a.png", MIMEType: "image/png", SizeBytes: 11 << 20}}}, false},
		{"photo not an image", PublishRequest{Kind: PublishPhoto, Media: []MediaRef{{URL: "https://r2/a.mp4", MIMEType: "video/mp4"}}}, false},
		{"photo over http", PublishRequest{Kind: PublishPhoto, Media: []MediaRef{image("http://r2/a.jpg")}}, false},
		{"two photos as photo", PublishRequest{Kind: PublishPhoto, Media: []MediaRef{image("https://r2/a.jpg"), image("https://r2/b.jpg")}}, false},
		{"album", PublishRequest{Kind: PublishAlbum, Media: []MediaRef{image("https://r2/a.jpg"), image("https://r2/b.jpg")}}, true},
		{"album of one", PublishRequest{Kind: PublishAlbum, Media: []MediaRef{image("https://r2/a.jpg")}}, false},
		{"album of eleven", PublishRequest{Kind: PublishAlbum, Media: func() []MediaRef {
			out := make([]MediaRef, 11)
			for i := range out {
				out[i] = image("https://r2/x.jpg")
			}
			return out
		}()}, false},
		{"scheduled in an hour", PublishRequest{Kind: PublishText, Message: "x", ScheduledAt: in(60)}, true},
		{"scheduled too soon", PublishRequest{Kind: PublishText, Message: "x", ScheduledAt: in(5)}, false},
		{"scheduled too far", PublishRequest{Kind: PublishText, Message: "x", ScheduledAt: in(76 * 24 * 60)}, false},
		{"message too long", PublishRequest{Kind: PublishText, Message: strings.Repeat("a", MaxPostMessageRunes+1)}, false},
		{"unknown kind", PublishRequest{Kind: "carousel", Message: "x"}, false},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			err := tc.req.Validate(postNow)
			if tc.ok && err != nil {
				t.Fatalf("unexpected error %v", err)
			}
			if !tc.ok && !errors.Is(err, ErrInvalidPost) {
				t.Fatalf("got %v, want ErrInvalidPost", err)
			}
		})
	}
}

func mp4(url string) MediaRef {
	return MediaRef{URL: url, MIMEType: "video/mp4", SizeBytes: 50 << 20}
}

func TestVideoReelAndStoryValidation(t *testing.T) {
	soon := postNow.Add(time.Hour)
	farVideo := postNow.Add(170 * 24 * time.Hour)
	cases := []struct {
		name string
		req  PublishRequest
		ok   bool
	}{
		{"video", PublishRequest{Kind: PublishVideo, VideoTitle: "Lançamento", Message: "Veja", Media: []MediaRef{mp4("https://r2/v.mp4")}}, true},
		{"video scheduled months ahead", PublishRequest{Kind: PublishVideo, Media: []MediaRef{mp4("https://r2/v.mp4")}, ScheduledAt: &farVideo}, true},
		{"video that is an image", PublishRequest{Kind: PublishVideo, Media: []MediaRef{image("https://r2/a.jpg")}}, false},
		{"video without media", PublishRequest{Kind: PublishVideo}, false},
		{"reel", PublishRequest{Kind: PublishReel, Message: "novidade", Media: []MediaRef{mp4("https://r2/r.mp4")}}, true},
		{"reel scheduled", PublishRequest{Kind: PublishReel, Media: []MediaRef{mp4("https://r2/r.mp4")}, ScheduledAt: &soon}, true},
		{"two reels", PublishRequest{Kind: PublishReel, Media: []MediaRef{mp4("https://r2/a.mp4"), mp4("https://r2/b.mp4")}}, false},
		{"photo story", PublishRequest{Kind: PublishStory, Media: []MediaRef{image("https://r2/a.jpg")}}, true},
		{"video story", PublishRequest{Kind: PublishStory, Media: []MediaRef{mp4("https://r2/s.mp4")}}, true},
		{"scheduled story", PublishRequest{Kind: PublishStory, Media: []MediaRef{image("https://r2/a.jpg")}, ScheduledAt: &soon}, false},
		{"story with text", PublishRequest{Kind: PublishStory, Message: "oi", Media: []MediaRef{image("https://r2/a.jpg")}}, false},
		{"title on a photo", PublishRequest{Kind: PublishPhoto, VideoTitle: "x", Media: []MediaRef{image("https://r2/a.jpg")}}, false},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			err := tc.req.Validate(postNow)
			if tc.ok && err != nil {
				t.Fatalf("unexpected error %v", err)
			}
			if !tc.ok && !errors.Is(err, ErrInvalidPost) {
				t.Fatalf("got %v, want ErrInvalidPost", err)
			}
		})
	}
}

func TestPostScheduleLeadStaysAtSeventyFiveDaysForNonVideo(t *testing.T) {
	far := postNow.Add(100 * 24 * time.Hour)
	if err := (PublishRequest{Kind: PublishText, Message: "x", ScheduledAt: &far}).Validate(postNow); !errors.Is(err, ErrInvalidPost) {
		t.Fatalf("got %v", err)
	}
}

func TestStoryAndVideoKindsMapToTheirMedia(t *testing.T) {
	if !(PublishRequest{Kind: PublishStory, Media: []MediaRef{mp4("https://r2/s.mp4")}}).IsVideoStory() {
		t.Fatal("an mp4 story is a video story")
	}
	if (PublishRequest{Kind: PublishStory, Media: []MediaRef{image("https://r2/a.jpg")}}).IsVideoStory() {
		t.Fatal("an image story is a photo story")
	}
}

func TestJobStatusMachine(t *testing.T) {
	if !JobQueued.CanTransitionTo(JobUploading) || !JobUploading.CanTransitionTo(JobPublished) || !JobUploading.CanTransitionTo(JobScheduled) {
		t.Fatal("the happy path must be allowed")
	}
	if JobPublished.CanTransitionTo(JobUploading) || JobFailed.CanTransitionTo(JobQueued) {
		t.Fatal("terminal jobs must stay terminal")
	}
	if !JobFailed.Terminal() || !JobPublished.Terminal() || !JobScheduled.Terminal() || JobUploading.Terminal() {
		t.Fatal("terminal set is wrong")
	}
}

func TestRemotePostKind(t *testing.T) {
	cases := []struct {
		name string
		post RemotePost
		want PostKind
	}{
		{"status", RemotePost{FromID: "PAGE"}, PostStatus},
		{"photo", RemotePost{FromID: "PAGE", Attachments: []RemoteAttachment{{Type: "photo", MediaType: "photo"}}}, PostPhoto},
		{"album", RemotePost{FromID: "PAGE", Attachments: []RemoteAttachment{{Type: "album"}}}, PostAlbum},
		{"video", RemotePost{FromID: "PAGE", Attachments: []RemoteAttachment{{Type: "video_inline", MediaType: "video"}}}, PostVideo},
		{"link", RemotePost{FromID: "PAGE", Attachments: []RemoteAttachment{{Type: "share"}}}, PostLink},
		{"visitor", RemotePost{FromID: "USER"}, PostVisitor},
	}
	for _, tc := range cases {
		if got := tc.post.KindFor("PAGE"); got != tc.want {
			t.Errorf("%s = %q, want %q", tc.name, got, tc.want)
		}
	}
}

func TestOnlyAppMadePostsAreEditable(t *testing.T) {
	if (&Post{CreatedByApp: false}).Editable() || !(&Post{CreatedByApp: true}).Editable() {
		t.Fatal("editability follows CreatedByApp")
	}
}

func TestAPageOwnsOnlyItsOwnPostIDs(t *testing.T) {
	page := &Page{FBPageID: "123"}
	if !page.OwnsPostID("123_456") || page.OwnsPostID("1234_456") || page.OwnsPostID("999_123") || (&Page{}).OwnsPostID("_1") {
		t.Fatal("post ownership must follow the page id prefix")
	}
}

func TestAClaimedJobCanBeReleasedForRetry(t *testing.T) {
	if !JobUploading.CanTransitionTo(JobQueued) || JobQueued.CanTransitionTo(JobQueued) {
		t.Fatal("only an uploading job returns to the queue")
	}
}
