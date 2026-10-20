package mercadolivre

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"net/url"
	"regexp"
	"strconv"
	"strings"

	mldomain "vozko/domain/mercadolivre"
)

const tagPostSale = "post_sale"

var (
	messageIDPattern    = regexp.MustCompile(`^[0-9A-Za-z]{16,64}$`)
	attachmentIDPattern = regexp.MustCompile(`^[A-Za-z0-9][A-Za-z0-9_.-]{0,199}$`)
)

func validAttachmentID(id string) bool {
	return attachmentIDPattern.MatchString(id) && !strings.Contains(id, "..")
}

type MessageService struct {
	c *Client
}

var _ mldomain.MessageService = (*MessageService)(nil)

func NewMessageService(c *Client) (*MessageService, error) {
	if c == nil {
		return nil, errors.New("mercadolivre: message service requires a client")
	}
	return &MessageService{c: c}, nil
}

type conversationStatusWire struct {
	Path       string              `json:"path"`
	Status     string              `json:"status"`
	Substatus  *string             `json:"substatus"`
	ClaimID    flexIDs             `json:"claim_id"`
	ClaimIDs   flexIDs             `json:"claim_ids"`
	ShippingID mldomain.FlexibleID `json:"shipping_id"`
}

type messageDateWire struct {
	Received flexTime `json:"received"`
	Created  flexTime `json:"created"`
	Read     flexTime `json:"read"`
}

type moderationWire struct {
	Status string `json:"status"`
	Reason string `json:"reason"`
}

type attachmentWire struct {
	Filename         string `json:"filename"`
	OriginalFilename string `json:"original_filename"`
	Type             string `json:"type"`
	Size             int64  `json:"size"`
}

type resourceWire struct {
	ID   string `json:"id"`
	Name string `json:"name"`
}

type messageWire struct {
	ID             string           `json:"id"`
	MessageID      string           `json:"message_id"`
	From           userRef          `json:"from"`
	To             userRef          `json:"to"`
	Status         string           `json:"status"`
	Text           flexText         `json:"text"`
	MessageDate    messageDateWire  `json:"message_date"`
	Moderation     *moderationWire  `json:"message_moderation"`
	LegacyMod      *moderationWire  `json:"moderation"`
	Attachments    []attachmentWire `json:"message_attachments"`
	Resources      []resourceWire   `json:"message_resources"`
	ConversationID string           `json:"conversation_id"`
	Date           flexTime         `json:"date"`
	DateRead       flexTime         `json:"date_read"`
	Resource       string           `json:"resource"`
	ResourceID     string           `json:"resource_id"`
}

func (w messageWire) toDomain() *mldomain.RemoteMessage {
	m := &mldomain.RemoteMessage{
		ID:               w.ID,
		FromUserID:       w.From.UserID.String(),
		ToUserID:         w.To.UserID.String(),
		Text:             string(w.Text),
		Status:           mldomain.MessageStatus(strings.TrimSpace(w.Status)),
		CreatedAt:        w.MessageDate.Created.Time,
		ReadAt:           w.MessageDate.Read.ptr(),
		ConversationPath: w.ConversationID,
	}
	if m.ID == "" {
		m.ID = w.MessageID
	}
	if !w.MessageDate.Created.Set {
		m.CreatedAt = w.Date.Time
		if !w.Date.Set {
			m.CreatedAt = w.MessageDate.Received.Time
		}
	}
	if m.ReadAt == nil {
		m.ReadAt = w.DateRead.ptr()
	}
	mod := w.Moderation
	if mod == nil {
		mod = w.LegacyMod
	}
	if mod != nil {
		m.ModerationStatus = mod.Status
		m.ModerationReason = mod.Reason
	}
	for _, a := range w.Attachments {
		if a.Filename == "" {
			continue
		}
		m.Attachments = append(m.Attachments, mldomain.RemoteAttachment{
			ID:           a.Filename,
			OriginalName: a.OriginalFilename,
			ContentType:  a.Type,
			Size:         a.Size,
		})
	}
	for _, r := range w.Resources {
		if r.Name == "packs" {
			m.PackKey = r.ID
		}
	}
	return m
}

type threadWire struct {
	Paging                 *paging                 `json:"paging"`
	ConversationStatus     *conversationStatusWire `json:"conversation_status"`
	Messages               []messageWire           `json:"messages"`
	SellerMaxMessageLength int                     `json:"seller_max_message_length"`
}

func validPack(packKey string) error {
	if !mldomain.ValidNumericID(packKey) {
		return fmt.Errorf("%w: %q", mldomain.ErrPackUnknown, packKey)
	}
	return nil
}

func (s *MessageService) Thread(ctx context.Context, creds mldomain.Credentials, query mldomain.ThreadQuery) (*mldomain.Thread, error) {
	return s.thread(ctx, creds, query, false)
}

func (s *MessageService) ThreadForOperator(ctx context.Context, creds mldomain.Credentials, query mldomain.ThreadQuery) (*mldomain.Thread, error) {
	return s.thread(ctx, creds, query, true)
}

func (s *MessageService) thread(ctx context.Context, creds mldomain.Credentials, query mldomain.ThreadQuery, markRead bool) (*mldomain.Thread, error) {
	if err := validPack(query.PackKey); err != nil {
		return nil, err
	}
	q := url.Values{}
	q.Set("tag", tagPostSale)
	if !markRead {
		q.Set("mark_as_read", "false")
	}
	q.Set("offset", strconv.Itoa(max(query.Offset, 0)))
	q.Set("limit", strconv.Itoa(boundLimit(query.Limit, mldomain.MaxThreadPageLimit)))
	var w threadWire
	if err := s.c.getJSON(ctx, request{
		path:   "/messages/packs/" + pathEscape(query.PackKey) + "/sellers/" + pathEscape(creds.UserID),
		query:  q,
		family: FamilyMessagesRead,
		creds:  creds,
	}, &w); err != nil {
		return nil, err
	}
	th := &mldomain.Thread{SellerMaxMessageLength: w.SellerMaxMessageLength}
	if cs := w.ConversationStatus; cs != nil {
		th.Status = strings.TrimSpace(cs.Status)
		if cs.Substatus != nil {
			th.Substatus = strings.TrimSpace(*cs.Substatus)
		}
		th.Path = cs.Path
		th.ClaimIDs = append(append([]string{}, cs.ClaimID...), cs.ClaimIDs...)
		th.ShippingID = cs.ShippingID.String()
	}
	if w.Paging != nil {
		th.Total, th.Offset, th.Limit = w.Paging.Total, w.Paging.Offset, w.Paging.Limit
	}
	for _, m := range w.Messages {
		th.Messages = append(th.Messages, m.toDomain())
	}
	return th, nil
}

func (s *MessageService) GetMessage(ctx context.Context, creds mldomain.Credentials, messageID string) (*mldomain.RemoteMessage, error) {
	if !messageIDPattern.MatchString(messageID) {
		return nil, fmt.Errorf("%w: message %q", mldomain.ErrInvalidResource, messageID)
	}
	q := url.Values{}
	q.Set("tag", tagPostSale)
	q.Set("mark_as_read", "false")
	var w struct {
		messageWire
		Messages []messageWire `json:"messages"`
	}
	if err := s.c.getJSON(ctx, request{
		path:   "/messages/" + pathEscape(messageID),
		query:  q,
		family: FamilyMessagesRead,
		creds:  creds,
	}, &w); err != nil {
		return nil, err
	}
	var m *mldomain.RemoteMessage
	if len(w.Messages) > 0 {
		m = w.Messages[0].toDomain()
	} else {
		m = w.messageWire.toDomain()
	}
	if m.ID == "" {
		return nil, &Error{Code: CodeDecode, Message: "message answer carries no message"}
	}
	return m, nil
}

type sendBody struct {
	From        sendParty `json:"from"`
	To          sendParty `json:"to"`
	Text        string    `json:"text"`
	Attachments []string  `json:"attachments,omitempty"`
}

type sendParty struct {
	UserID string `json:"user_id"`
}

type sentWire struct {
	ID         string          `json:"id"`
	MessageID  string          `json:"message_id"`
	Status     string          `json:"status"`
	Moderation *moderationWire `json:"message_moderation"`
	Date       messageDateWire `json:"message_date"`
}

func (w sentWire) toDomain(status int) *mldomain.SentMessage {
	sent := &mldomain.SentMessage{
		ID:            w.ID,
		Status:        mldomain.MessageStatus(strings.TrimSpace(w.Status)),
		PendingReview: status == http.StatusAccepted,
		CreatedAt:     w.Date.Created.Time,
	}
	if sent.ID == "" {
		sent.ID = w.MessageID
	}
	if sent.Status == mldomain.MessagePendingReview {
		sent.PendingReview = true
	}
	if w.Moderation != nil {
		sent.ModerationStatus = w.Moderation.Status
		sent.ModerationReason = w.Moderation.Reason
	}
	return sent
}

func (s *MessageService) Send(ctx context.Context, creds mldomain.Credentials, msg mldomain.SendMessage) (*mldomain.SentMessage, error) {
	if err := validPack(msg.PackKey); err != nil {
		return nil, err
	}
	if !mldomain.ValidNumericID(msg.ToUserID) || msg.ToUserID == creds.UserID {
		return nil, mldomain.ErrBuyerUnknown
	}
	if err := mldomain.ValidateOutgoingMessage(msg.Text, len(msg.AttachmentIDs)); err != nil {
		return nil, err
	}
	for _, id := range msg.AttachmentIDs {
		if !validAttachmentID(id) {
			return nil, fmt.Errorf("%w: attachment id %q", mldomain.ErrMediaUnsupported, id)
		}
	}
	q := url.Values{}
	q.Set("tag", tagPostSale)
	var w sentWire
	resp, err := s.c.sendJSON(ctx, request{
		method: http.MethodPost,
		path:   "/messages/packs/" + pathEscape(msg.PackKey) + "/sellers/" + pathEscape(creds.UserID),
		query:  q,
		family: FamilyMessagesWrite,
		creds:  creds,
	}, sendBody{
		From:        sendParty{UserID: creds.UserID},
		To:          sendParty{UserID: msg.ToUserID},
		Text:        msg.Text,
		Attachments: msg.AttachmentIDs,
	}, &w)
	if err != nil {
		return nil, markAcceptedButUnreadable(resp, err)
	}
	return w.toDomain(resp.status), nil
}

type unreadWire struct {
	Results []struct {
		Resource string `json:"resource"`
		Count    int    `json:"count"`
	} `json:"results"`
}

func (s *MessageService) Unread(ctx context.Context, creds mldomain.Credentials) ([]mldomain.UnreadThread, error) {
	q := url.Values{}
	q.Set("role", "seller")
	q.Set("tag", tagPostSale)
	var w unreadWire
	if err := s.c.getJSON(ctx, request{
		path:   "/messages/unread",
		query:  q,
		family: FamilyMessagesRead,
		creds:  creds,
	}, &w); err != nil {
		return nil, err
	}
	out := make([]mldomain.UnreadThread, 0, len(w.Results))
	for _, r := range w.Results {
		pack, seller, err := mldomain.ParseThreadResource(r.Resource)
		if err != nil {
			return nil, err
		}
		out = append(out, mldomain.UnreadThread{PackKey: pack, SellerID: seller, Count: r.Count})
	}
	return out, nil
}
