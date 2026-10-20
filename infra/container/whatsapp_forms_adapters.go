package container

import (
	"context"
	"errors"
	"fmt"
	"io"
	"net/http"
	"strings"
	"time"

	conversation_domain "vozko/domain/conversation"
	"vozko/domain/lead"
	"vozko/domain/media"
	"vozko/domain/shared"
	businessphone "vozko/domain/whatsapp/business_phone"
	"vozko/domain/whatsapp_campaign_entry"
	wf "vozko/domain/whatsappform"
	conversation_usecase "vozko/usecases/conversation"
	whatsappform_usecase "vozko/usecases/whatsappform"
)

const (
	formPhonesPageSize   = 100
	formImageFetchTimeout = 15 * time.Second
)

type formOwnedPhones struct {
	phones businessphone.Repository
}

func (p formOwnedPhones) OwnedPhones(_ context.Context, workspaceID string) ([]*businessphone.WhatsAppBusinessPhoneNumber, error) {
	var out []*businessphone.WhatsAppBusinessPhoneNumber
	for page := 1; ; page++ {
		result, err := p.phones.List(businessphone.ListInput{
			OwnerWorkspaceID: workspaceID,
			Options:          shared.QueryOptions{Pagination: shared.Pagination{Page: page, PageSize: formPhonesPageSize}},
		})
		if err != nil {
			return nil, err
		}
		for _, phone := range result.Items {
			if phone != nil && phone.BelongsToWorkspace(workspaceID) {
				out = append(out, phone)
			}
		}
		if len(result.Items) < formPhonesPageSize {
			return out, nil
		}
	}
}

type formAssets struct {
	media  media.MediaRepository
	client *http.Client
}

func (a formAssets) Load(ctx context.Context, workspaceID string, mediaIDs []string) (wf.Assets, error) {
	out := wf.Assets{}
	if len(mediaIDs) == 0 {
		return out, nil
	}
	items, err := a.media.GetMediasByIDs(mediaIDs)
	if err != nil {
		return nil, err
	}
	for _, item := range items {
		if item.WorkspaceID != workspaceID || item.Type.Private() {
			continue
		}
		data, err := a.fetch(ctx, item.URL)
		if err != nil {
			return nil, fmt.Errorf("image %s of the form: %w", item.ID, err)
		}
		out[item.ID] = data
	}
	return out, nil
}

func (a formAssets) fetch(ctx context.Context, url string) ([]byte, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
	if err != nil {
		return nil, err
	}
	resp, err := a.client.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("storage answered %d", resp.StatusCode)
	}
	return io.ReadAll(io.LimitReader(resp.Body, wf.MaxImageBytes+1))
}

type formSender struct {
	clients conversation_domain.WhatsAppClientFactory
}

func (s formSender) SendFlowMessage(ctx context.Context, phoneID string, in conversation_domain.SendFlowMessageInput) (string, error) {
	client, err := s.clients.ClientForPhone(phoneID)
	if err != nil {
		return "", err
	}
	out, err := client.SendFlowMessage(ctx, in)
	if err != nil {
		return "", err
	}
	if out == nil || strings.TrimSpace(out.MessageID) == "" {
		return "", conversation_domain.ErrSendOutcomeUnknown
	}
	return out.MessageID, nil
}

type formConversations struct {
	sender *conversation_usecase.MessageSenderService
}

func (c formConversations) Target(_ context.Context, workspaceID, entryID string) (whatsappform_usecase.ConversationTarget, error) {
	target, err := c.sender.OfficialTarget(entryID)
	switch {
	case errors.Is(err, conversation_usecase.ErrEntryWorkspaceUnresolved), errors.Is(err, whatsapp_campaign_entry.ErrEntryNotFound), errors.Is(err, lead.ErrLeadNotFound):
		return whatsappform_usecase.ConversationTarget{}, wf.ErrChannelUnsupported
	case err != nil:
		return whatsappform_usecase.ConversationTarget{}, err
	case target.WorkspaceID != workspaceID:
		return whatsappform_usecase.ConversationTarget{}, wf.ErrForbidden
	}
	return whatsappform_usecase.ConversationTarget{
		EntryID: entryID, EntryType: string(shared.EntryTypeWhatsApp), LeadID: target.LeadID,
		BusinessPhoneID: target.BusinessPhoneID, ContactNumber: target.LeadNumber, WindowOpen: target.WindowOpen,
	}, nil
}

type formHistory struct {
	sender *conversation_usecase.MessageSenderService
}

func (h formHistory) RecordFormMessage(_ context.Context, out whatsappform_usecase.OutboundForm) error {
	_, err := h.sender.RecordFormMessage(conversation_usecase.FormMessageRecord{
		EntryID: out.EntryID, To: out.To, MessageID: out.MessageID, Body: out.Body, SentBy: out.SentBy, Metadata: out.Metadata,
	})
	return err
}

type formFiles struct {
	store *conversation_usecase.FormFileStore
}

func (f formFiles) StoreFormFile(ctx context.Context, file whatsappform_usecase.StoredFile) (string, error) {
	return f.store.Store(ctx, conversation_usecase.FormFile{
		BusinessPhoneID: file.BusinessPhoneID, EntryID: file.EntryID, EntryType: shared.EntryType(file.EntryType),
		MediaID: file.File.ID, Name: file.File.Name, MimeType: file.File.MimeType, SHA256: file.File.SHA256,
	})
}

type formReplies struct {
	forms *whatsappform_usecase.Service
}

func (r formReplies) ReceiveFormReply(ctx context.Context, in conversation_usecase.InboundFormReply) (conversation_usecase.ReceivedFormReply, error) {
	received, err := r.forms.Receive(ctx, whatsappform_usecase.InboundForm{
		WorkspaceID: in.WorkspaceID, BusinessPhoneID: in.BusinessPhoneID, ContactNumber: in.ContactNumber,
		EntryID: in.EntryID, EntryType: in.EntryType, ConversationID: in.ConversationID, LeadID: in.LeadID,
		InboundMessageID: in.InboundMessageID, ContextMessageID: in.ContextMessageID, ResponseJSON: in.ResponseJSON, SubmittedAt: in.SubmittedAt,
	})
	if err != nil {
		return conversation_usecase.ReceivedFormReply{}, err
	}
	return conversation_usecase.ReceivedFormReply{Text: received.Text, Metadata: received.Metadata, ResumedRunID: received.ResumedRunID}, nil
}
