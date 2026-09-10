package container

import (
	"context"

	iguc "vozko/usecases/instagram"
)

// instagramCommentReplier adapts Instagram's existing reply use case onto the
// narrow CommentReplier port the comment engine declares (§6).
//
// A rename, essentially, and deliberately nothing more. The scope check
// (CanManageComments), the empty-text refusal and the write into the local
// comment mirror all already live in that use case; a second call path to the
// Graph edge would have to repeat all three and would drift from them.
type instagramCommentReplier struct {
	uc *iguc.ReplyToCommentUseCase
}

func (r instagramCommentReplier) ReplyToComment(ctx context.Context, workspaceID, accountID, sourceCommentID, text string) (string, error) {
	return r.uc.Execute(ctx, workspaceID, accountID, sourceCommentID, text)
}
