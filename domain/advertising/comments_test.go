package advertising

import (
	"errors"
	"testing"
)

func TestCommentsAreReadFromFacebookUnlessInstagramIsAsked(t *testing.T) {
	cases := map[string]string{"": PlatformFacebook, "facebook": PlatformFacebook, " Instagram ": PlatformInstagram}
	for raw, want := range cases {
		got, err := CommentPlatformOf(raw)
		if err != nil || got != want {
			t.Fatalf("%q: %q %v", raw, got, err)
		}
	}
	if _, err := CommentPlatformOf("tiktok"); !errors.Is(err, ErrUnknownCommentChannel) {
		t.Fatalf("tiktok: %v", err)
	}
}

func TestAnAdPointsToItsPostOnEachPlatform(t *testing.T) {
	posts := AdPosts{FacebookPostID: "1348090328393443_99", InstagramMediaID: "1790"}
	if id, err := posts.PostOn(PlatformFacebook); err != nil || id != "1348090328393443_99" {
		t.Fatalf("facebook %q %v", id, err)
	}
	if id, err := posts.PostOn(PlatformInstagram); err != nil || id != "1790" {
		t.Fatalf("instagram %q %v", id, err)
	}
	if posts.PageID() != "1348090328393443" {
		t.Fatalf("page %q", posts.PageID())
	}
}

func TestAnAdWithoutAPostSaysSoInsteadOfShowingNoComments(t *testing.T) {
	if _, err := (AdPosts{FacebookPostID: "1_2"}).PostOn(PlatformInstagram); !errors.Is(err, ErrAdHasNoPost) {
		t.Fatalf("instagram: %v", err)
	}
	if _, err := (AdPosts{FacebookPostID: "99"}).PostOn(PlatformFacebook); !errors.Is(err, ErrAdHasNoPost) {
		t.Fatalf("a post id without its page was read: %v", err)
	}
}

func TestOnlyAdsHaveComments(t *testing.T) {
	if err := (&Object{Level: LevelAd}).CommentsAllowed(); err != nil {
		t.Fatal(err)
	}
	if err := (&Object{Level: LevelAdSet}).CommentsAllowed(); !errors.Is(err, ErrCommentsOnlyForAds) {
		t.Fatalf("ad set: %v", err)
	}
}
