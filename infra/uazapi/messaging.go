package uazapi

import (
	"context"
	"fmt"
	"net/http"
	"strings"

	uw "vozko/domain/unofficial_whatsapp"
)

var _ uw.MessagingAPI = (*Client)(nil)

type sendEnvelope struct {
	Number      string `json:"number"`
	ReplyID     string `json:"replyid,omitempty"`
	Delay       int    `json:"delay,omitempty"`
	TrackSource string `json:"track_source,omitempty"`
	TrackID     string `json:"track_id,omitempty"`
}

func envelopeFor(chatID, replyID string, delayMS int, trackSource, trackID string) sendEnvelope {
	return sendEnvelope{
		Number: chatID, ReplyID: replyID, Delay: delayMS,
		TrackSource: trackSource, TrackID: trackID,
	}
}

type sendResponse struct {
	ID        string `json:"id"`
	MessageID string `json:"messageid"`
	Status    string `json:"status"`
}

func (r sendResponse) result() *uw.SendResult {
	return &uw.SendResult{
		ProviderMessageID: firstNonEmpty(r.MessageID, r.ID),
		Status:            uw.DeliveryStatus(strings.ToLower(r.Status)),
	}
}

type sendTextRequest struct {
	sendEnvelope
	Text string `json:"text"`
}

func (c *Client) SendText(ctx context.Context, ref uw.InstanceRef, in uw.SendTextInput) (*uw.SendResult, error) {
	var resp sendResponse
	err := c.instanceCall(ctx, ref, http.MethodPost, "/send/text", sendTextRequest{
		sendEnvelope: envelopeFor(in.ChatID, in.ReplyToProviderMessageID, in.DelayMS, in.TrackSource, in.TrackID),
		Text:         in.Text,
	}, &resp)
	if err != nil {
		return nil, err
	}
	return resp.result(), nil
}

type sendMediaRequest struct {
	sendEnvelope
	Type     string `json:"type"`
	File     string `json:"file"`
	Text     string `json:"text,omitempty"`
	DocName  string `json:"docName,omitempty"`
	MIMEType string `json:"mimetype,omitempty"`
}

func (c *Client) SendMedia(ctx context.Context, ref uw.InstanceRef, in uw.SendMediaInput) (*uw.SendResult, error) {
	file := in.URL
	if file == "" {
		file = in.Base64
	}
	if file == "" {
		return nil, fmt.Errorf("uazapi: media send needs a URL or base64 payload")
	}

	var resp sendResponse
	err := c.instanceCall(ctx, ref, http.MethodPost, "/send/media", sendMediaRequest{
		sendEnvelope: envelopeFor(in.ChatID, in.ReplyToProviderMessageID, in.DelayMS, in.TrackSource, in.TrackID),
		Type:         providerMediaType(in.Kind),
		File:         file,
		Text:         in.Caption,
		DocName:      in.FileName,
		MIMEType:     in.MIMEType,
	}, &resp)
	if err != nil {
		return nil, err
	}
	return resp.result(), nil
}

func providerMediaType(kind uw.MediaKind) string {
	switch kind {
	case uw.MediaImage:
		return "image"
	case uw.MediaVideo:
		return "video"
	case uw.MediaAudio:
		return "audio"
	case uw.MediaVoice:
		return "ptt"
	case uw.MediaSticker:
		return "sticker"
	default:
		return "document"
	}
}

type sendMenuRequest struct {
	sendEnvelope
	Type       string   `json:"type"`
	Text       string   `json:"text"`
	FooterText string   `json:"footerText,omitempty"`
	ListButton string   `json:"listButton,omitempty"`
	Choices    []string `json:"choices"`
}

func (c *Client) SendMenu(ctx context.Context, ref uw.InstanceRef, in uw.SendMenuInput) (*uw.SendResult, error) {
	menuType := "button"
	if in.Style == uw.InteractiveStyleList {
		menuType = "list"
	}

	var resp sendResponse
	err := c.instanceCall(ctx, ref, http.MethodPost, "/send/menu", sendMenuRequest{
		sendEnvelope: envelopeFor(in.ChatID, "", in.DelayMS, in.TrackSource, in.TrackID),
		Type:         menuType,
		Text:         in.Body,
		FooterText:   in.Footer,
		ListButton:   in.Button,
		Choices:      encodeChoices(in.Options, menuType == "list"),
	}, &resp)
	if err != nil {
		return nil, err
	}
	return resp.result(), nil
}

func encodeChoices(options []uw.InteractiveOption, withDescription bool) []string {
	out := make([]string, 0, len(options))
	for _, opt := range options {
		title := strings.ReplaceAll(opt.Title, "|", "/")
		id := strings.ReplaceAll(opt.ID, "|", "/")
		choice := title + "|" + id
		if withDescription && opt.Description != "" {
			choice += "|" + strings.ReplaceAll(opt.Description, "|", "/")
		}
		out = append(out, choice)
	}
	return out
}

type presenceRequest struct {
	Number   string `json:"number"`
	Presence string `json:"presence"`
	Delay    int    `json:"delay,omitempty"`
}

func (c *Client) SendPresence(ctx context.Context, ref uw.InstanceRef, chatID string, presence uw.Presence, delayMS int) error {
	if delayMS > uw.MaxPresenceMS {
		delayMS = uw.MaxPresenceMS
	}
	return c.instanceCall(ctx, ref, http.MethodPost, "/message/presence", presenceRequest{
		Number: chatID, Presence: string(presence), Delay: delayMS,
	}, nil)
}

func (c *Client) MarkRead(ctx context.Context, ref uw.InstanceRef, providerMessageIDs []string) error {
	if len(providerMessageIDs) == 0 {
		return nil
	}
	return c.instanceCall(ctx, ref, http.MethodPost, "/message/markread",
		map[string]any{"id": providerMessageIDs}, nil)
}

func (c *Client) React(ctx context.Context, ref uw.InstanceRef, chatID, providerMessageID, emoji string) error {
	return c.instanceCall(ctx, ref, http.MethodPost, "/message/react",
		map[string]any{"number": chatID, "id": providerMessageID, "text": emoji}, nil)
}

func (c *Client) EditMessage(ctx context.Context, ref uw.InstanceRef, providerMessageID, text string) (*uw.SendResult, error) {
	var resp sendResponse
	err := c.instanceCall(ctx, ref, http.MethodPost, "/message/edit",
		map[string]any{"id": providerMessageID, "text": text}, &resp)
	if err != nil {
		return nil, err
	}
	return resp.result(), nil
}

func (c *Client) DeleteMessage(ctx context.Context, ref uw.InstanceRef, providerMessageID string) error {
	return c.instanceCall(ctx, ref, http.MethodPost, "/message/delete",
		map[string]any{"id": providerMessageID}, nil)
}

type downloadResponse struct {
	FileURL    string `json:"fileURL"`
	MIMEType   string `json:"mimetype"`
	Base64Data string `json:"base64Data"`
}

func (c *Client) DownloadMedia(ctx context.Context, ref uw.InstanceRef, providerMessageID string) (*uw.RemoteMedia, error) {
	var resp downloadResponse
	err := c.instanceCall(ctx, ref, http.MethodPost, "/message/download", map[string]any{
		"id":            providerMessageID,
		"return_base64": true,
		"return_link":   false,
		"transcribe":    false,
		"generate_mp3":  false,
	}, &resp)
	if err != nil {
		return nil, err
	}

	data, err := decodeBase64Payload(resp.Base64Data)
	if err != nil {
		return nil, fmt.Errorf("uazapi: decode media payload: %w", err)
	}
	return &uw.RemoteMedia{Data: data, MIMEType: resp.MIMEType, URL: resp.FileURL}, nil
}

type numberCheckResponse struct {
	Query        string `json:"query"`
	JID          string `json:"jid"`
	LID          string `json:"lid"`
	IsInWhatsApp bool   `json:"isInWhatsapp"`
	VerifiedName string `json:"verifiedName"`
}

func (c *Client) CheckNumbers(ctx context.Context, ref uw.InstanceRef, numbers []string) ([]uw.NumberCheck, error) {
	if len(numbers) == 0 {
		return nil, nil
	}
	var raw []numberCheckResponse
	err := c.instanceCall(ctx, ref, http.MethodPost, "/chat/check",
		map[string]any{"numbers": numbers}, &raw)
	if err != nil {
		return nil, err
	}
	out := make([]uw.NumberCheck, 0, len(raw))
	for _, item := range raw {
		out = append(out, uw.NumberCheck{
			Query: item.Query, JID: item.JID, LID: item.LID,
			IsOnWhatsApp: item.IsInWhatsApp, VerifiedName: item.VerifiedName,
		})
	}
	return out, nil
}
