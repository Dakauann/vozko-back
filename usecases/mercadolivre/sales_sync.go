package mercadolivre

import (
	"context"
	"errors"
	"fmt"

	mldomain "vozko/domain/mercadolivre"
)

type SalesSync struct {
	conversations mldomain.ConversationRepository
	svc           Services
	summaries     *PackSummaries
	threads       *ThreadSync
}

func NewSalesSync(conversations mldomain.ConversationRepository, svc Services, tokens *TokenSource, summaries *PackSummaries, threads *ThreadSync) *SalesSync {
	if conversations == nil || tokens == nil || summaries == nil || threads == nil || svc.Orders == nil || svc.Messages == nil {
		panic("mercadolivre: the sales sync requires conversations, orders, messages, tokens, pack summaries and the thread sync")
	}
	return &SalesSync{conversations: conversations, svc: svc, summaries: summaries, threads: threads}
}

func (s *SalesSync) Handle(ctx context.Context, account *mldomain.Account, job mldomain.Job) error {
	creds, err := s.threads.credentials(ctx, account)
	if err != nil {
		return err
	}
	conv, err := s.conversationFor(ctx, account, creds, job)
	if errors.Is(err, mldomain.ErrConversationNotFound) {
		return nil
	}
	if err != nil {
		return err
	}
	if err := s.summaries.Refresh(ctx, creds, conv); err != nil {
		return s.threads.apiErr(ctx, account, err)
	}
	if job.Kind == mldomain.JobRefreshPack {
		return nil
	}
	thread, err := s.svc.Messages.Thread(ctx, creds, mldomain.ThreadQuery{PackKey: conv.PackKey, Limit: 1})
	if err != nil {
		return s.threads.apiErr(ctx, account, err)
	}
	return s.threads.applyState(ctx, account, conv, thread, nil)
}

func (s *SalesSync) conversationFor(ctx context.Context, account *mldomain.Account, creds mldomain.Credentials, job mldomain.Job) (*mldomain.Conversation, error) {
	switch job.Kind {
	case mldomain.JobFetchOrder:
		order, err := s.svc.Orders.GetOrder(ctx, creds, job.ResourceID)
		if err != nil {
			return nil, s.threads.apiErr(ctx, account, err)
		}
		packKey, err := mldomain.PackKeyFor(order.PackID, order.ID)
		if err != nil {
			return nil, err
		}
		return s.conversations.FindByPack(ctx, account.ID, packKey)
	case mldomain.JobFetchShipment:
		return s.conversations.FindByShipment(ctx, account.ID, job.ResourceID)
	case mldomain.JobRefreshPack:
		return s.conversations.FindByPack(ctx, account.ID, job.ResourceID)
	}
	return nil, fmt.Errorf("%w: %s", ErrJobKindNotHandled, job.Kind)
}
