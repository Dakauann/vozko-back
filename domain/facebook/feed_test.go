package facebook

import (
	"encoding/json"
	"testing"
	"time"
)

func feedEvent(t *testing.T, field, value string) *FeedEvent {
	t.Helper()
	ev, err := NormalizeFeedChange(field, json.RawMessage(value))
	if err != nil {
		t.Fatalf("normalize %s: %v", value, err)
	}
	return ev
}

func TestCommentVerbsMapToCommentEvents(t *testing.T) {
	cases := map[string]FeedKind{
		"add":    FeedCommentAdded,
		"edited": FeedCommentEdited,
		"edit":   FeedCommentEdited,
		"remove": FeedCommentRemoved,
		"delete": FeedCommentRemoved,
		"hide":   FeedCommentHidden,
		"unhide": FeedCommentUnhidden,
		"block":  FeedUnknown,
	}
	for verb, want := range cases {
		ev := feedEvent(t, "feed", `{"item":"comment","verb":"`+verb+`","comment_id":"P_C","post_id":"PAGE_P","parent_id":"PAGE_P","from":{"id":"PSID","name":"Tester"},"created_time":1449135003,"message":"oi"}`)
		if ev.Kind != want {
			t.Errorf("%s gave %q, want %q", verb, ev.Kind, want)
		}
	}
}

func TestCommentCarriesItsThreadAndAuthor(t *testing.T) {
	ev := feedEvent(t, "feed", `{"item":"comment","verb":"add","comment_id":"P_C2","post_id":"PAGE_P","parent_id":"P_C1","from":{"id":"PSID","name":"Tester"},"created_time":1449135003,"message":"resposta","photo":"https://scontent/x.jpg"}`)
	if ev.CommentID != "P_C2" || ev.PostID != "PAGE_P" || ev.ParentID != "P_C1" || ev.Message != "resposta" || ev.Photo == "" {
		t.Fatalf("event = %+v", ev)
	}
	if ev.IsTopLevelComment() {
		t.Fatal("a reply to a comment is not top level")
	}
	if ev.From == nil || ev.From.ID != "PSID" || ev.From.Name != "Tester" {
		t.Fatalf("from = %+v", ev.From)
	}
	if !ev.CreatedTime.Equal(time.Unix(1449135003, 0).UTC()) {
		t.Fatalf("created = %v", ev.CreatedTime)
	}
}

func TestLegacySenderFieldsStandInForFrom(t *testing.T) {
	ev := feedEvent(t, "feed", `{"item":"comment","verb":"add","comment_id":"C","post_id":"P","parent_id":"P","sender_id":123,"sender_name":"Antigo"}`)
	if ev.From == nil || ev.From.ID != "123" || ev.From.Name != "Antigo" {
		t.Fatalf("from = %+v", ev.From)
	}
	if !ev.IsTopLevelComment() {
		t.Fatal("parent equal to post is top level")
	}
}

func TestMissingAuthorStaysNil(t *testing.T) {
	ev := feedEvent(t, "feed", `{"item":"comment","verb":"remove","comment_id":"C","post_id":"P","parent_id":"P"}`)
	if ev.From != nil {
		t.Fatalf("from = %+v", ev.From)
	}
}

func TestPostItemsMapToPostEvents(t *testing.T) {
	for _, item := range []string{"status", "post", "photo", "video", "share", "link", "album"} {
		ev := feedEvent(t, "feed", `{"item":"`+item+`","verb":"add","post_id":"PAGE_P","from":{"id":"PAGE","name":"Loja"},"published":0,"created_time":1448633038000}`)
		if ev.Kind != FeedPostAdded {
			t.Errorf("%s gave %q", item, ev.Kind)
		}
		if ev.Published == nil || *ev.Published {
			t.Errorf("%s published = %v", item, ev.Published)
		}
		if !ev.IsFromPage("PAGE") || ev.IsFromPage("OTHER") {
			t.Errorf("%s page authorship wrong", item)
		}
		if !ev.CreatedTime.Equal(time.UnixMilli(1448633038000).UTC()) {
			t.Errorf("%s created = %v", item, ev.CreatedTime)
		}
	}
	for verb, want := range map[string]FeedKind{"edited": FeedPostEdited, "remove": FeedPostRemoved, "hide": FeedPostHidden, "unhide": FeedPostUnhidden} {
		if ev := feedEvent(t, "feed", `{"item":"status","verb":"`+verb+`","post_id":"P"}`); ev.Kind != want {
			t.Errorf("post %s gave %q", verb, ev.Kind)
		}
	}
}

func TestPublishedAcceptsBooleansAndNumbers(t *testing.T) {
	for raw, want := range map[string]bool{"1": true, "0": false, "true": true, "false": false} {
		ev := feedEvent(t, "feed", `{"item":"status","verb":"add","post_id":"P","published":`+raw+`}`)
		if ev.Published == nil || *ev.Published != want {
			t.Errorf("published %s = %v", raw, ev.Published)
		}
	}
	if ev := feedEvent(t, "feed", `{"item":"status","verb":"add","post_id":"P"}`); ev.Published != nil {
		t.Error("absent published must stay unknown")
	}
}

func TestReactionTargetsTheCommentWhenPresent(t *testing.T) {
	post := feedEvent(t, "feed", `{"item":"reaction","verb":"add","post_id":"PAGE_P","parent_id":"PAGE_P","reaction_type":"love","from":{"id":"PSID"}}`)
	if post.Kind != FeedReaction || post.ReactionTarget() != "PAGE_P" || post.ReactionType != "love" || post.Verb != "add" {
		t.Fatalf("post reaction = %+v", post)
	}
	comment := feedEvent(t, "feed", `{"item":"reaction","verb":"remove","post_id":"PAGE_P","comment_id":"P_C","reaction_type":"like"}`)
	if comment.ReactionTarget() != "P_C" {
		t.Fatalf("comment reaction target = %q", comment.ReactionTarget())
	}
}

func TestMentionAndVideoFields(t *testing.T) {
	mention := feedEvent(t, "mention", `{"message":"@Loja","post_id":"U_P","comment_id":"P_C","created_time":1614340862,"item":"comment","verb":"add"}`)
	if mention.Kind != FeedMentioned || mention.PostID != "U_P" || mention.CommentID != "P_C" {
		t.Fatalf("mention = %+v", mention)
	}
	video := feedEvent(t, "videos", `{"id":"V1","status":{"video_status":"ready"}}`)
	if video.Kind != FeedVideoStatus || video.VideoID != "V1" || video.VideoStatus != "ready" {
		t.Fatalf("video = %+v", video)
	}
}

func TestUnknownFieldsAreKeptRaw(t *testing.T) {
	ev := feedEvent(t, "ratings", `{"x":1}`)
	if ev.Kind != FeedUnknown || string(ev.Raw) != `{"x":1}` {
		t.Fatalf("event = %+v", ev)
	}
}

func TestMalformedFeedValueIsAnError(t *testing.T) {
	if _, err := NormalizeFeedChange("feed", json.RawMessage(`{"item":`)); err == nil {
		t.Fatal("expected a decode error")
	}
}
