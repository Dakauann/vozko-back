package marketing

import (
	"context"
	"testing"

	"vozko/domain/advertising"
)

func TestGetAdPostsReadsTheEffectivePostOfEachPlatform(t *testing.T) {
	g, calls := gatewayWith(t, ok(`{"id":"A1","creative":{"id":"C1","effective_object_story_id":"P1_9","effective_instagram_media_id":"M7"}}`))
	posts, err := g.GetAdPosts(context.Background(), "sys", "A1")
	if err != nil {
		t.Fatal(err)
	}
	if posts != (advertising.AdPosts{FacebookPostID: "P1_9", InstagramMediaID: "M7"}) || (*calls)[0].query.Get("fields") != adPostFields {
		t.Fatalf("posts = %+v calls = %+v", posts, *calls)
	}
}

func TestGetAdPostsOfAnAdWithoutCreativeIsEmpty(t *testing.T) {
	g, _ := gatewayWith(t, ok(`{"id":"A1"}`))
	posts, err := g.GetAdPosts(context.Background(), "sys", "A1")
	if err != nil || posts != (advertising.AdPosts{}) {
		t.Fatalf("posts = %+v err = %v", posts, err)
	}
}

func TestListPostCommentsReadsTheNewestWithThePageToken(t *testing.T) {
	g, calls := gatewayWith(t, routes(t, map[string]string{
		"GET /v26.0/P1": `{"access_token":"page-tok","id":"P1"}`,
		"GET /v26.0/P1_9/comments": `{"data":[{"id":"P1_9_1","message":"Quanto custa?","from":{"name":"Ana"},"created_time":"2026-10-03T12:00:00+0000","like_count":2,"comment_count":1},
			{"id":"P1_9_2","message":"Oi","created_time":"2026-10-03T11:00:00+0000"}]}`,
	}))
	comments, err := g.ListPostComments(context.Background(), "sys", "P1", "P1_9")
	if err != nil {
		t.Fatal(err)
	}
	if len(comments) != 2 || comments[0].AuthorName != "Ana" || comments[0].LikeCount != 2 || comments[0].ReplyCount != 1 || comments[1].AuthorName != "" {
		t.Fatalf("comments = %+v", comments)
	}
	call := (*calls)[1]
	if (*calls)[0].path != "/v26.0/P1" || call.query.Get("order") != "reverse_chronological" || call.query.Get("limit") != "100" {
		t.Fatalf("call = %+v", call)
	}
}

func TestListInstagramCommentsCountsReplies(t *testing.T) {
	g, calls := gatewayWith(t, ok(`{"data":[{"id":"IC1","text":"Amei","username":"loja.x","timestamp":"2026-10-03T12:00:00+0000","like_count":4,"replies":{"summary":{"total_count":3}}}]}`))
	comments, err := g.ListInstagramComments(context.Background(), "sys", "M7")
	if err != nil {
		t.Fatal(err)
	}
	if len(comments) != 1 || comments[0].AuthorName != "loja.x" || comments[0].ReplyCount != 3 || comments[0].CreatedAt == nil || (*calls)[0].path != "/v26.0/M7/comments" {
		t.Fatalf("comments = %+v", comments)
	}
}

func TestACommentWithAnUnreadableDateFails(t *testing.T) {
	g, _ := gatewayWith(t, ok(`{"data":[{"id":"IC1","text":"Amei","timestamp":"ontem"}]}`))
	if _, err := g.ListInstagramComments(context.Background(), "sys", "M7"); err == nil {
		t.Fatal("an unreadable date was accepted")
	}
}
