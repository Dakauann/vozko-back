package mercadolivre

import (
	"context"
	"errors"
	"fmt"
	"log"
	"mime"
	"path"
	"strings"
	"time"

	"vozko/domain/channel"
	"vozko/domain/conversation"
	mldomain "vozko/domain/mercadolivre"
	"vozko/domain/shared"
)

const staleRereadBucket = 10 * time.Minute

type ChannelAdapterDeps struct {
	Accounts      mldomain.AccountRepository
	Contacts      mldomain.ContactRepository
	Conversations mldomain.ConversationRepository
	Services      Services
	Tokens        *TokenSource
	Threads       *ThreadSync
	Refresh       *JobPublisher
	Media         mldomain.MediaFetcher
	Metrics       Metrics
}

type ChannelAdapter struct {
	accounts      mldomain.AccountRepository
	contacts      mldomain.ContactRepository
	conversations mldomain.ConversationRepository
	svc           Services
	tokens        *TokenSource
	threads       *ThreadSync
	refresh       *JobPublisher
	media         mldomain.MediaFetcher
	metrics       Metrics
	caps          channel.Capabilities
	now           Clock
}

var (
	_ conversation.ChannelAdapter      = (*ChannelAdapter)(nil)
	_ conversation.CapabilitiesAdapter = (*ChannelAdapter)(nil)
)

func NewChannelAdapter(d ChannelAdapterDeps) *ChannelAdapter {
	if d.Accounts == nil || d.Contacts == nil || d.Conversations == nil || d.Tokens == nil || d.Threads == nil || d.Refresh == nil || d.Media == nil || d.Metrics == nil ||
		d.Services.Messages == nil || d.Services.Attachments == nil || d.Services.Errors == nil {
		panic("mercadolivre: the channel adapter is missing a dependency")
	}
	return &ChannelAdapter{
		accounts: d.Accounts, contacts: d.Contacts, conversations: d.Conversations, svc: d.Services,
		tokens: d.Tokens, threads: d.Threads, refresh: d.Refresh, media: d.Media, metrics: d.Metrics,
		caps: mldomain.Descriptor().Capabilities, now: utcNow,
	}
}

func (a *ChannelAdapter) EntryType() shared.EntryType { return shared.EntryTypeMercadoLivre }

func (a *ChannelAdapter) Capabilities() channel.Capabilities { return a.caps }

func (a *ChannelAdapter) ResolveEntry(ctx context.Context, entryID string) (*conversation.EntryContext, error) {
	conv, err := a.conversations.FindByID(ctx, entryID)
	if err != nil {
		return nil, err
	}
	contact, err := a.contacts.FindByID(ctx, conv.ContactID)
	if err != nil {
		return nil, err
	}
	return &conversation.EntryContext{
		EntryID:       conv.ID,
		EntryType:     shared.EntryTypeMercadoLivre,
		WorkspaceID:   conv.WorkspaceID,
		AccountID:     conv.AccountID,
		ContactID:     contact.ID,
		ContactRef:    contact.MLUserID,
		ContactHandle: contact.Nickname,
		LastInboundAt: conv.LastCustomerMessageAt,
	}, nil
}

func (a *ChannelAdapter) WindowState(ctx context.Context, ec *conversation.EntryContext) (conversation.WindowState, error) {
	if ec == nil {
		return conversation.ClosedWindow(conversation.WindowReasonChannelUnavailable), conversation.ErrNoAdapterForEntryType
	}
	account, conv, err := a.load(ctx, ec)
	if errors.Is(err, mldomain.ErrAccountNotFound) {
		return conversation.ClosedWindow(conversation.WindowReasonChannelUnavailable), nil
	}
	if err != nil {
		return conversation.ClosedWindow(conversation.WindowReasonChannelUnavailable), err
	}
	return a.window(ctx, account, conv), nil
}

func (a *ChannelAdapter) AutomationDisclosure(ctx context.Context, ec *conversation.EntryContext) (string, error) {
	if ec == nil {
		return "", conversation.ErrNoAdapterForEntryType
	}
	account, err := a.accounts.FindByID(ctx, ec.AccountID)
	if err != nil {
		return "", err
	}
	return account.Automation().Disclosure, nil
}

var _ conversation.DisclosingAdapter = (*ChannelAdapter)(nil)

func (a *ChannelAdapter) load(ctx context.Context, ec *conversation.EntryContext) (*mldomain.Account, *mldomain.Conversation, error) {
	account, err := a.accounts.FindByID(ctx, ec.AccountID)
	if err != nil {
		return nil, nil, err
	}
	conv, err := a.conversations.FindByID(ctx, ec.EntryID)
	if err != nil {
		return nil, nil, err
	}
	if conv.AccountID != account.ID {
		return nil, nil, mldomain.ErrConversationNotFound
	}
	return account, conv, nil
}

func (a *ChannelAdapter) window(ctx context.Context, account *mldomain.Account, conv *mldomain.Conversation) conversation.WindowState {
	if account.CanSend() && conv.StatusIsStale(a.now()) {
		a.queueReread(account, conv)
	}
	return a.windowOf(account, conv)
}

func (a *ChannelAdapter) windowOf(account *mldomain.Account, conv *mldomain.Conversation) conversation.WindowState {
	if account == nil || !account.CanSend() {
		return conversation.ClosedWindow(conversation.WindowReasonChannelUnavailable)
	}
	if !conv.WindowOpen {
		if conv.AgentMediated != nil && mldomain.KnownSubstatus(conv.MLSubstatus) {
			return conversation.ClosedByProvider(conv.MLSubstatus)
		}
		return conversation.ClosedWindow(conversation.WindowReasonChannelUnavailable)
	}
	caps := a.caps.WithOutboundBudget(conv.OutboundBudget)
	return conversation.EvaluateWindow(caps, conv.LastCustomerMessageAt, conv.OutboundSinceInbound, a.now(), true)
}

func (a *ChannelAdapter) queueReread(account *mldomain.Account, conv *mldomain.Conversation) {
	marker := "stale:" + a.now().Truncate(staleRereadBucket).Format("200601021504")
	job, err := mldomain.ThreadJob(account.MLUserID, conv.PackKey, marker)
	if err == nil {
		err = a.refresh.Publish(job)
	}
	if err != nil {
		log.Printf("[mercadolivre][send] stale status of %s kept, re-read not queued: %v", conv.ID, err)
	}
}

func (a *ChannelAdapter) SendText(ctx context.Context, ec *conversation.EntryContext, req conversation.SendTextRequest) (*conversation.SendOutcome, error) {
	if err := mldomain.ValidateMessageText(req.Body); err != nil {
		return nil, err
	}
	return a.send(ctx, ec, req.Body, nil)
}

func (a *ChannelAdapter) SendMedia(ctx context.Context, ec *conversation.EntryContext, req conversation.SendMediaRequest) (*conversation.SendOutcome, error) {
	caption := strings.TrimSpace(req.Caption)
	if err := mldomain.ValidateOutgoingMessage(caption, 1); err != nil {
		return nil, err
	}
	upload, err := a.attachment(ctx, req)
	if err != nil {
		return nil, errors.Join(conversation.ErrCapabilityUnsupported, err)
	}
	return a.send(ctx, ec, caption, &upload)
}

func (a *ChannelAdapter) attachment(ctx context.Context, req conversation.SendMediaRequest) (mldomain.AttachmentUpload, error) {
	data, contentType := req.Bytes, strings.TrimSpace(req.MIMEType)
	name := strings.TrimSpace(req.FileName)
	if len(data) == 0 {
		if strings.TrimSpace(req.URL) == "" {
			return mldomain.AttachmentUpload{}, fmt.Errorf("%w: the attachment has no file", mldomain.ErrMediaUnsupported)
		}
		file, err := a.media.Fetch(ctx, req.URL, mldomain.MaxAttachmentBytes)
		if err != nil {
			return mldomain.AttachmentUpload{}, err
		}
		data = file.Data
		if contentType == "" {
			contentType = file.ContentType
		}
		if name == "" {
			name = path.Base(strings.SplitN(req.URL, "?", 2)[0])
		}
	}
	if name == "" || name == "." || name == "/" {
		name = "anexo" + extensionFor(contentType)
	}
	if err := mldomain.ValidateAttachment(name, contentType, int64(len(data))); err != nil {
		return mldomain.AttachmentUpload{}, err
	}
	return mldomain.AttachmentUpload{FileName: name, ContentType: contentType, Data: data}, nil
}

func extensionFor(contentType string) string {
	if exts, err := mime.ExtensionsByType(contentType); err == nil && len(exts) > 0 {
		return exts[0]
	}
	return ""
}

func (a *ChannelAdapter) send(ctx context.Context, ec *conversation.EntryContext, text string, upload *mldomain.AttachmentUpload) (*conversation.SendOutcome, error) {
	if ec == nil {
		return nil, conversation.ErrNoAdapterForEntryType
	}
	account, conv, err := a.load(ctx, ec)
	if errors.Is(err, mldomain.ErrAccountNotFound) {
		return nil, errors.Join(conversation.ErrOutboundWindowClosed, err)
	}
	if err != nil {
		return nil, err
	}
	if window := a.window(ctx, account, conv); !window.Open {
		a.metrics.AddMercadoLivreEvent("send", "refused_"+string(window.Reason))
		return nil, closedError(window)
	}
	target, err := mldomain.SendTarget(conv, account.SiteID, account.MLUserID)
	if err != nil {
		a.metrics.AddMercadoLivreEvent("send", "refused_target")
		return nil, errors.Join(conversation.ErrOutboundWindowClosed, err)
	}
	creds, err := a.tokens.Credentials(ctx, account)
	if err != nil {
		if errors.Is(err, mldomain.ErrNeedsReconnect) {
			return nil, errors.Join(conversation.ErrOutboundWindowClosed, err)
		}
		return nil, err
	}
	var attachmentIDs []string
	if upload != nil {
		id, err := a.svc.Attachments.Upload(ctx, creds, *upload)
		if err != nil {
			return nil, a.refused(ctx, account, conv, err, false)
		}
		attachmentIDs = []string{id}
	}
	claimed, err := a.conversations.ClaimOutbound(ctx, conv.ID)
	if err != nil {
		return nil, fmt.Errorf("mercadolivre: the send budget could not be claimed: %w", err)
	}
	if !claimed {
		a.metrics.AddMercadoLivreEvent("send", "refused_claim")
		return nil, conversation.ErrOutboundWindowClosed
	}
	sent, err := a.svc.Messages.Send(ctx, creds, mldomain.SendMessage{PackKey: conv.PackKey, ToUserID: target, Text: text, AttachmentIDs: attachmentIDs})
	if err != nil {
		return nil, a.refused(ctx, account, conv, err, true)
	}
	a.metrics.AddMercadoLivreEvent("send", "sent")
	if err := a.conversations.RecordOutbound(ctx, conv.ID, a.now()); err != nil {
		a.metrics.AddMercadoLivreEvent("send", "outbound_clock_failed")
		log.Printf("[mercadolivre][send] outbound clock of %s not recorded: %v", conv.ID, err)
	}
	outcome := &conversation.SendOutcome{
		ProviderMessageID: sent.ID,
		Metadata:          map[string]any{ModerationMetadataKey: moderationValue(sent.Status, sent.ModerationStatus, sent.ModerationReason)},
	}
	if moderationFailed(sent.Status, sent.ModerationStatus) {
		a.metrics.AddMercadoLivreEvent("moderation", "rejected")
		outcome.DeliveryStatus = conversation.DeliveryStatusFailed
	}
	return outcome, nil
}

func (a *ChannelAdapter) refused(ctx context.Context, account *mldomain.Account, conv *mldomain.Conversation, err error, claimed bool) error {
	errs := a.svc.Errors
	definite := errs.IsDefiniteRefusal(err)
	if claimed && definite {
		if releaseErr := a.conversations.ReleaseOutbound(ctx, conv.ID); releaseErr != nil {
			log.Printf("[mercadolivre][send] claim on %s not released: %v", conv.ID, releaseErr)
		} else {
			a.metrics.AddMercadoLivreEvent("send", "claim_released")
		}
	}
	if substatus, blocked := errs.Blocked(err); blocked {
		a.metrics.AddMercadoLivreEvent("send", "blocked")
		if applyErr := a.threads.ApplyBlock(ctx, account, conv, substatus); applyErr != nil {
			log.Printf("[mercadolivre][send] block of %s not mirrored: %v", conv.ID, applyErr)
		}
		return errors.Join(conversation.ErrOutboundWindowClosed, fmt.Errorf("mercadolivre: the thread is blocked (%s): %w", substatus, err))
	}
	if errs.IsAuth(err) {
		if _, recoverErr := a.tokens.RecoverFromAuthFailure(ctx, account); recoverErr != nil {
			log.Printf("[mercadolivre][send] token of %s not recovered: %v", account.ID, recoverErr)
		}
	}
	outcome := "failed"
	if !definite {
		outcome = "ambiguous"
	}
	a.metrics.AddMercadoLivreEvent("send", outcome)
	return fmt.Errorf("mercadolivre: send to %s failed: %w", conv.ID, err)
}

func closedError(window conversation.WindowState) error {
	if window.ProviderDetail == "" {
		return conversation.ErrOutboundWindowClosed
	}
	return errors.Join(conversation.ErrOutboundWindowClosed, fmt.Errorf("mercadolivre: %s", window.ProviderDetail))
}
