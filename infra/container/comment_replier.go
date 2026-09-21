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
