package audience_usecase

import (
	"context"
	"fmt"
	"strings"

	ca "vozko/domain/audience"
)

type escalateCommentUseCase struct {
	repo     ca.Repository
	adapters map[ca.Source]ca.SourceAdapter
	sender   ca.EscalationSender
}

func NewEscalateCommentUseCase(repo ca.Repository, adapters map[ca.Source]ca.SourceAdapter, sender ca.EscalationSender) ca.EscalateCommentUseCase {
	return &escalateCommentUseCase{repo: repo, adapters: adapters, sender: sender}
}

func (uc *escalateCommentUseCase) Execute(ctx context.Context, in ca.EscalateCommentInput) (*ca.Escalation, error) {
	if uc.sender == nil {
		return nil, fmt.Errorf("%w: no channel is configured to send escalations", ca.ErrInvalidFilter)
	}
	recipient := strings.TrimSpace(in.RecipientID)
	if recipient == "" {
		return nil, fmt.Errorf("%w: a recipient is required", ca.ErrInvalidFilter)
	}

	comment, err := uc.repo.FindByID(ctx, strings.TrimSpace(in.WorkspaceID), strings.TrimSpace(in.CommentID))
	if err != nil {
		return nil, err
	}

	escalation := ca.NewEscalation(comment, uc.permalink(ctx, comment), in.Note)
	if err := escalation.Validate(); err != nil {
		return nil, err
	}

	if err := uc.sender.Send(ctx, ca.EscalationDelivery{
		WorkspaceID:   escalation.WorkspaceID,
		RecipientID:   recipient,
		RecipientKind: strings.TrimSpace(in.RecipientKind),
		ActorUserID:   strings.TrimSpace(in.UserID),
		Text:          escalation.Message(),
	}); err != nil {
		return nil, err
	}
	return &escalation, nil
}

func (uc *escalateCommentUseCase) permalink(ctx context.Context, c *ca.Analysis) string {
	adapter, ok := uc.adapters[c.Source]
	if !ok || adapter == nil {
		return ""
	}
	container, err := adapter.ReadContainerContext(ctx, ca.ContainerRef{
		Source: c.Source, AccountID: c.AccountID, ContainerID: c.ContainerID,
	})
	if err != nil {
		return ""
	}
	return container.Permalink
}
