package container

import (
	"context"

	iguc "vozko/usecases/instagram"
)

type instagramCommentReplier struct {
	uc *iguc.ReplyToCommentUseCase
}

func (r instagramCommentReplier) ReplyToComment(ctx context.Context, workspaceID, accountID, sourceCommentID, text string) (string, error) {
	return r.uc.Execute(ctx, workspaceID, accountID, sourceCommentID, text)
}

type facebookCommentReplier struct {
	bundle *facebookBundle
}

func (r facebookCommentReplier) ReplyToComment(ctx context.Context, workspaceID, pageID, fbCommentID, text string) (string, error) {
	return r.bundle.CommentsUC.Reply(ctx, workspaceID, pageID, fbCommentID, text)
}
