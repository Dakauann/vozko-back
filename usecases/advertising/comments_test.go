package advertising

import (
	"context"
	"errors"
	"testing"
	"time"

	ads "vozko/domain/advertising"
)

func commentsWorld() (*world, *CommentsUseCase) {
	w := newWorld()
	w.objects.byID["ad-1"] = &ads.Object{MetaID: "ad-1", WorkspaceID: "ws-1", AdAccountID: "acc-1", Level: ads.LevelAd}
	w.objects.byID["set-1"] = &ads.Object{MetaID: "set-1", WorkspaceID: "ws-1", AdAccountID: "acc-1", Level: ads.LevelAdSet}
	w.gateway.posts = ads.AdPosts{FacebookPostID: "page-1_77", InstagramMediaID: "media-9"}
	return w, NewCommentsUseCase(w.sync, w.gateway)
}

func at(hour int) *time.Time {
	t := time.Date(2026, 10, 3, hour, 0, 0, 0, time.UTC)
	return &t
}

func TestCommentsOfAnAdComeFromItsFacebookPostNewestFirst(t *testing.T) {
	w, uc := commentsWorld()
	w.gateway.comments = []ads.AdComment{{ID: "old", CreatedAt: at(9)}, {ID: "undated"}, {ID: "new", CreatedAt: at(12)}}
	comments, err := uc.List(context.Background(), "ws-1", "ad-1", "")
	if err != nil {
		t.Fatal(err)
	}
	if len(comments) != 3 || comments[0].ID != "new" || comments[1].ID != "old" || comments[2].ID != "undated" {
		t.Fatalf("comments %+v", comments)
	}
	if countCalls(w.gateway.calls, "facebook_comments:page-1:page-1_77") != 1 {
		t.Fatalf("calls %v", w.gateway.calls)
	}
}

func TestInstagramCommentsComeFromTheAdsInstagramMedia(t *testing.T) {
	w, uc := commentsWorld()
	if _, err := uc.List(context.Background(), "ws-1", "ad-1", "instagram"); err != nil {
		t.Fatal(err)
	}
	if countCalls(w.gateway.calls, "instagram_comments:media-9") != 1 {
		t.Fatalf("calls %v", w.gateway.calls)
	}
}

func TestCommentsAreRefusedBeforeAnyMetaCall(t *testing.T) {
	cases := map[string]struct {
		workspace, metaID, platform string
		want                        error
	}{
		"another workspace": {"ws-2", "ad-1", "facebook", ads.ErrObjectNotFound},
		"an ad set":         {"ws-1", "set-1", "facebook", ads.ErrCommentsOnlyForAds},
		"another channel":   {"ws-1", "ad-1", "tiktok", ads.ErrUnknownCommentChannel},
	}
	for name, c := range cases {
		w, uc := commentsWorld()
		if _, err := uc.List(context.Background(), c.workspace, c.metaID, c.platform); !errors.Is(err, c.want) {
			t.Fatalf("%s: %v", name, err)
		}
		if len(w.gateway.calls) != 0 {
			t.Fatalf("%s: calls %v", name, w.gateway.calls)
		}
	}
}

func TestAnAdWithoutAnInstagramPostSaysSo(t *testing.T) {
	w, uc := commentsWorld()
	w.gateway.posts = ads.AdPosts{FacebookPostID: "page-1_77"}
	if _, err := uc.List(context.Background(), "ws-1", "ad-1", "instagram"); !errors.Is(err, ads.ErrAdHasNoPost) {
		t.Fatalf("err %v", err)
	}
}

func TestAMissingCommentPermissionIsExplainedWithoutAskingToReconnect(t *testing.T) {
	w, uc := commentsWorld()
	w.gateway.failOn, w.gateway.failWith = "instagram_comments:media-9", &ads.RemoteError{Kind: ads.FailurePermission, Code: 10, Message: "Application does not have permission"}
	if _, err := uc.List(context.Background(), "ws-1", "ad-1", "instagram"); !errors.Is(err, ads.ErrCommentsNotAllowed) {
		t.Fatalf("err %v", err)
	}
	if w.accounts.byID["acc-1"].Connection != ads.ConnectionConnected {
		t.Fatal("a missing comment permission asked to reconnect the account")
	}
}
