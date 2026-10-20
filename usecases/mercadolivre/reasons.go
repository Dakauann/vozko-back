package mercadolivre

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log"
	"strings"

	"vozko/domain/conversation"
	mldomain "vozko/domain/mercadolivre"
	"vozko/domain/shared"
)

const reasonMetadataKey = "mercadolivre_reason"

type ThreadRefresher interface {
	RefreshState(ctx context.Context, account *mldomain.Account, conv *mldomain.Conversation) error
}

type ReasonsDeps struct {
	Accounts      mldomain.AccountRepository
	Conversations mldomain.ConversationRepository
	Services      Services
	Tokens        *TokenSource
	Threads       ThreadRefresher
	History       conversation.MessageHistoryManager
	Access        shared.EntryAccessChecker
	Metrics       Metrics
}

type Reasons struct {
	accounts      mldomain.AccountRepository
	conversations mldomain.ConversationRepository
	svc           Services
	tokens        *TokenSource
	threads       ThreadRefresher
	history       conversation.MessageHistoryManager
	access        shared.EntryAccessChecker
	metrics       Metrics
	now           Clock
}

func NewReasons(d ReasonsDeps) *Reasons {
	if d.Accounts == nil || d.Conversations == nil || d.Tokens == nil || d.Threads == nil || d.History == nil || d.Access == nil ||
		d.Metrics == nil || d.Services.ActionGuide == nil || d.Services.Errors == nil {
		panic("mercadolivre: reasons require accounts, conversations, the action guide, an error classifier, tokens, the thread sync, history, entry access and metrics")
	}
	return &Reasons{
		accounts: d.Accounts, conversations: d.Conversations, svc: d.Services, tokens: d.Tokens, threads: d.Threads,
		history: d.History, access: d.Access, metrics: d.Metrics, now: utcNow,
	}
}

type ConversationQuery struct {
	Actor       conversation.SentBy
	SystemAdmin bool
	WorkspaceID string
	EntryID     string
}

type SendReasonInput struct {
	ConversationQuery
	OptionID   string
	TemplateID string
	Text       string
	Vars       []mldomain.ReasonVarValue
}

type SendReasonOutput struct {
	ProviderMessageID string
}

type reasonTarget struct {
	userID  string
	account *mldomain.Account
	conv    *mldomain.Conversation
	creds   mldomain.Credentials
}

type conversationGuard struct {
	access        shared.EntryAccessChecker
	conversations mldomain.ConversationRepository
}

func (g conversationGuard) authorize(ctx context.Context, q ConversationQuery) (string, *mldomain.Conversation, error) {
	userID := strings.TrimSpace(q.Actor.ID())
	if q.Actor.Kind() != conversation.SenderHuman || userID == "" {
		return "", nil, mldomain.ErrPersonOnly
	}
	person := shared.Person{UserID: userID, SystemAdmin: q.SystemAdmin}
	if !person.MayActOn(g.access, q.WorkspaceID, q.EntryID, string(shared.EntryTypeMercadoLivre)) {
		return "", nil, conversation.ErrUnauthorized
	}
	conv, err := g.conversations.FindByID(ctx, q.EntryID)
	if err != nil {
		return "", nil, err
	}
	if conv.WorkspaceID != q.WorkspaceID {
		return "", nil, mldomain.ErrConversationNotFound
	}
	return userID, conv, nil
}

func (r *Reasons) authorize(ctx context.Context, q ConversationQuery) (*reasonTarget, error) {
	userID, conv, err := conversationGuard{access: r.access, conversations: r.conversations}.authorize(ctx, q)
	if err != nil {
		return nil, err
	}
	account, err := r.accounts.FindByID(ctx, conv.AccountID)
	if err != nil {
		return nil, err
	}
	if !account.CanSend() {
		return nil, fmt.Errorf("%w: reasons", ErrAccountUnavailable)
	}
	creds, err := r.tokens.Credentials(ctx, account)
	if errors.Is(err, mldomain.ErrNeedsReconnect) {
		return nil, fmt.Errorf("%w: %v", ErrAccountUnavailable, err)
	}
	if err != nil {
		return nil, err
	}
	return &reasonTarget{userID: userID, account: account, conv: conv, creds: creds}, nil
}

func (r *Reasons) apiErr(ctx context.Context, account *mldomain.Account, err error) error {
	return classifyAPIError(ctx, r.svc.Errors, r.tokens, r.metrics, account, err)
}

func (r *Reasons) options(ctx context.Context, t *reasonTarget) ([]mldomain.ReasonOption, error) {
	options, err := r.svc.ActionGuide.Options(ctx, t.creds, t.conv.PackKey)
	if err != nil {
		return nil, r.apiErr(ctx, t.account, err)
	}
	caps, err := r.svc.ActionGuide.Caps(ctx, t.creds, t.conv.PackKey)
	if err != nil {
		return nil, r.apiErr(ctx, t.account, err)
	}
	return mldomain.ForSite(mldomain.WithCaps(options, caps), t.account.SiteID), nil
}

func (r *Reasons) Options(ctx context.Context, q ConversationQuery) ([]mldomain.ReasonOption, error) {
	target, err := r.authorize(ctx, q)
	if err != nil {
		return nil, err
	}
	return r.options(ctx, target)
}

func (r *Reasons) Send(ctx context.Context, in SendReasonInput) (*SendReasonOutput, error) {
	target, err := r.authorize(ctx, in.ConversationQuery)
	if err != nil {
		return nil, err
	}
	options, err := r.options(ctx, target)
	if err != nil {
		return nil, err
	}
	send := mldomain.ReasonSend{OptionID: in.OptionID, TemplateID: strings.TrimSpace(in.TemplateID), Text: in.Text, Vars: in.Vars}
	if err := mldomain.ValidateReasonSend(options, send); err != nil {
		r.metrics.AddMercadoLivreEvent("reasons", "refused")
		return nil, err
	}
	sent, err := r.svc.ActionGuide.SendOption(ctx, target.creds, target.conv.PackKey, send)
	if err != nil {
		r.metrics.AddMercadoLivreEvent("reasons", "failed")
		return nil, r.apiErr(ctx, target.account, err)
	}
	r.metrics.AddMercadoLivreEvent("reasons", "sent")
	r.record(ctx, target, send, sent)
	return &SendReasonOutput{ProviderMessageID: sent.ID}, nil
}

func (r *Reasons) record(ctx context.Context, t *reasonTarget, send mldomain.ReasonSend, sent *mldomain.RemoteMessage) {
	at := sent.CreatedAt
	if at.IsZero() {
		at = r.now()
	}
	text := sent.Text
	if strings.TrimSpace(text) == "" {
		text = send.Text
	}
	meta, err := json.Marshal(map[string]any{
		messageIDMetadataKey:  sent.ID,
		reasonMetadataKey:     send.OptionID,
		ModerationMetadataKey: moderationValue(sent.Status, sent.ModerationStatus, sent.ModerationReason),
	})
	if err == nil {
		err = r.history.Record(ctx, conversation.MessageHistoryRecord{
			SentBy:            conversation.SentByPerson(t.userID),
			EntryID:           t.conv.ID,
			EntryType:         shared.EntryTypeMercadoLivre,
			Channel:           conversation.MessageChannelMercadoLivre,
			MessageType:       conversation.MessageTypeOperator,
			ProviderMessageID: sent.ID,
			From:              t.account.MLUserID,
			To:                sent.ToUserID,
			Text:              text,
			Timestamp:         at,
			Metadata:          meta,
		})
	}
	if err != nil {
		r.metrics.AddMercadoLivreEvent("reasons", "record_failed")
		log.Printf("[mercadolivre][send] reason %s sent on %s but not recorded: %v", sent.ID, t.conv.ID, err)
	}
	if err := r.conversations.RecordOutbound(ctx, t.conv.ID, at); err != nil {
		log.Printf("[mercadolivre][send] outbound time of %s not stored: %v", t.conv.ID, err)
	}
	if err := r.threads.RefreshState(ctx, t.account, t.conv); err != nil {
		log.Printf("[mercadolivre][send] thread state of %s not refreshed after the reason: %v", t.conv.ID, err)
	}
}
