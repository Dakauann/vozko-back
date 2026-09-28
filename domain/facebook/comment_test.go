package facebook

import (
	"testing"
	"time"
)

func TestRemoteCommentByThePageIsOurs(t *testing.T) {
	page := &Page{ID: "p", WorkspaceID: "ws", FBPageID: "PAGE"}
	ours := CommentFromRemote(page, "PAGE_1", &RemoteComment{FBCommentID: "1_c", FromID: "PAGE", Message: "obrigado"})
	theirs := CommentFromRemote(page, "PAGE_1", &RemoteComment{FBCommentID: "1_d", FromID: "PSID", ParentID: "1_c", UserLikes: true})

	if !ours.IsOurs || !ours.FromIsPage || !ours.TopLevel() {
		t.Fatalf("ours = %+v", ours)
	}
	if theirs.IsOurs || theirs.TopLevel() || !theirs.LikedByPage || *theirs.FromID != "PSID" {
		t.Fatalf("theirs = %+v", theirs)
	}
}

func TestCommentWithoutAnAuthorHasNoContactRef(t *testing.T) {
	c := CommentFromRemote(&Page{FBPageID: "PAGE"}, "PAGE_1", &RemoteComment{FBCommentID: "x"})
	if c.FromID != nil || c.IsOurs {
		t.Fatalf("comment = %+v", c)
	}
}

func TestFeedCommentBecomesAComment(t *testing.T) {
	page := &Page{ID: "p", WorkspaceID: "ws", FBPageID: "PAGE"}
	ev := &FeedEvent{CommentID: "P_C", PostID: "PAGE_P", ParentID: "PAGE_P", Message: "preço?", Photo: "https://x",
		From: &Actor{ID: "PSID", Name: "Ana"}, CreatedTime: time.Unix(1449135003, 0).UTC()}

	c := page.CommentFromFeed(ev)
	if c.FBCommentID != "P_C" || c.FBPostID != "PAGE_P" || !c.TopLevel() || c.FromName != "Ana" || c.AttachmentType != "photo" || c.CreatedTime == nil || c.IsOurs {
		t.Fatalf("comment = %+v", c)
	}
}

func TestReactionVerbsMoveCounts(t *testing.T) {
	if ReactionDeltaFor("add") != 1 || ReactionDeltaFor("remove") != -1 || ReactionDeltaFor("edit") != 0 {
		t.Fatal("add counts, remove uncounts, edit changes only the type")
	}
}
