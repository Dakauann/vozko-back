package uazapi

import (
	"context"
	"fmt"
	"net/http"
	"strings"

	uw "vozko/domain/unofficial_whatsapp"
)

// The send and media half of the client.
//
// One vendor quirk shapes most of this file: every send endpoint accepts the
// same optional envelope (delay, reply, tracking), and the message TYPE is
// selected by a field rather than by the path. Building that envelope once,
// here, is what keeps the five send methods from drifting apart.

var _ uw.MessagingAPI = (*Client)(nil)

// sendEnvelope is the option set every send endpoint shares.
type sendEnvelope struct {
	Number  string `json:"number"`
	ReplyID string `json:"replyid,omitempty"`
	// Delay renders "Digitando…" for its duration. Always sent when non-zero:
	// human pacing is what keeps a number from looking automated.
	Delay int `json:"delay,omitempty"`
	// Async is deliberately absent (false). We need the real provider message id
	// synchronously to write external_message_id BEFORE the echo can arrive;
	// queueing would return a 200 with no id and make the echo insert a
	// duplicate.
	TrackSource string `json:"track_source,omitempty"`
	TrackID     string `json:"track_id,omitempty"`
}

func envelopeFor(chatID, replyID string, delayMS int, trackSource, trackID string) sendEnvelope {
	return sendEnvelope{
		Number: chatID, ReplyID: replyID, Delay: delayMS,
		TrackSource: trackSource, TrackID: trackID,
	}
}

// sendResponse is the message object every send endpoint answers with.
type sendResponse struct {
	ID        string `json:"id"`
	MessageID string `json:"messageid"`
	Status    string `json:"status"`
}

func (r sendResponse) result() *uw.SendResult {
	return &uw.SendResult{
		// Prefer the bare provider id over the composite "owner:messageid":
		// inbound webhooks carry the bare form, and storing the composite would
		// make the echo fail to match the row it should update.
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

// providerMediaType maps our normalized kinds onto the vendor's.
//
// A voice note is `ptt`, not `audio`: the vendor renders the two differently,
// and a customer receiving a voice message as a music player is a visible bug.
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

// encodeChoices renders options in the vendor's pipe-delimited form.
//
// The id is what a workflow branches on, so it must survive intact: a label
// containing a pipe would otherwise split into the wrong fields and silently
// change which option id comes back. Pipes in author-supplied text are replaced
// rather than escaped, because the vendor documents no escape syntax.
func encodeChoices(options []uw.InteractiveOption, withDescription bool) []string {
	out := make([]string, 0, len(options))
	for _, opt := range options {
		title := strings.ReplaceAll(opt.Title, "|", "/")
		id := strings.ReplaceAll(opt.ID, "|", "/")
		choice := title + "|" + id
		// Only list rows render a description; appending one to a button would
		// put stray text in the button's own id field.
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
	// The provider caps one presence request; a larger value is rejected rather
	// than clamped on their side.
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
	// An empty emoji REMOVES the reaction, which is the documented contract and
	// the reason this is one method rather than two.
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

// DownloadMedia fetches an inbound attachment.
//
// base64 rather than the provider's hosted link, deliberately: a third-party URL
// is not a durable asset and would leak tenant media to anyone holding it. The
// bytes go straight to our own object storage.
//
// `transcribe` is deliberately NOT requested. The provider can transcribe audio,
// but only by routing the customer's voice through its own model relay under a
// key we would have to store — and we already run speech-to-text ourselves.
func (c *Client) DownloadMedia(ctx context.Context, ref uw.InstanceRef, providerMessageID string) (*uw.RemoteMedia, error) {
	var resp downloadResponse
	err := c.instanceCall(ctx, ref, http.MethodPost, "/message/download", map[string]any{
		"id":            providerMessageID,
		"return_base64": true,
		"return_link":   false,
		"transcribe":    false,
		// OGG rather than a re-encoded MP3: the original is what the customer
		// sent, and a lossy conversion before speech-to-text costs accuracy.
		"generate_mp3": false,
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
