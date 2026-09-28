package facebook

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/url"
	"strings"
	"time"

	"vozko/domain/cache"
	fbdomain "vozko/domain/facebook"
	"vozko/infra/meta"
	"vozko/infra/meta/sendapi"
)

const (
	bucketText  = "fb_send_text"
	bucketMedia = "fb_send_media"
)

type MessagingConfig struct {
	Graph              GraphConfig
	RateLimiterFactory cache.RateLimiterFactory
}

type messagingService struct {
	client   *meta.Client
	throttle *meta.Throttle
}

func NewMessagingService(cfg MessagingConfig) (fbdomain.MessagingService, error) {
	client, err := newGraphClient(cfg.Graph, GraphHost)
	if err != nil {
		return nil, err
	}
	throttle, err := meta.NewThrottle(cfg.RateLimiterFactory,
		meta.Bucket{Name: bucketText, Max: 100, Window: time.Second},
		meta.Bucket{Name: bucketMedia, Max: 10, Window: time.Second},
	)
	if err != nil {
		return nil, err
	}
	return &messagingService{client: client, throttle: throttle}, nil
}

func (s *messagingService) Send(ctx context.Context, fbPageID, pageToken string, msg fbdomain.OutboundMessage) (*fbdomain.SendResult, error) {
	env, bucket, err := envelopeFor(msg)
	if err != nil {
		return nil, err
	}
	if err := s.throttle.Allow(bucket, fbPageID); err != nil {
		return nil, err
	}
	out, err := sendapi.Send(ctx, s.client, "/"+fbPageID+"/messages", pageToken, env)
	if err != nil {
		return nil, err
	}
	return &fbdomain.SendResult{RecipientID: out.RecipientID, MessageID: out.MessageID, AttachmentID: out.AttachmentID}, nil
}

func envelopeFor(msg fbdomain.OutboundMessage) (sendapi.Envelope, string, error) {
	to := recipientFor(msg.Recipient)
	bucket := bucketText
	var env sendapi.Envelope
	switch {
	case msg.AttachmentURL != "" || msg.AttachmentID != "":
		kind, err := sendapi.AttachmentTypeFor(msg.AttachmentKind)
		if err != nil {
			return env, "", err
		}
		if msg.AttachmentID != "" {
			env = sendapi.MediaByID(to, kind, msg.AttachmentID)
		} else {
			env = sendapi.Media(to, kind, msg.AttachmentURL)
		}
		bucket = bucketMedia
	case len(msg.Buttons) > 0:
		env = sendapi.Buttons(to, msg.Text, buttonsFor(msg.Buttons))
	case strings.TrimSpace(msg.Text) != "":
		env = sendapi.Text(to, msg.Text).
			WithQuickReplies(sendapi.QuickReplies(optionsFor(msg.QuickReplies), fbdomain.MaxQuickReplies, fbdomain.MaxOptionTitleRunes))
	default:
		return env, "", fmt.Errorf("facebook: a message needs text, buttons or an attachment")
	}

	if msg.Recipient.PSID != "" {
		switch msg.Tier {
		case fbdomain.SendHumanAgent:
			env = env.As(sendapi.MessagingTag, sendapi.TagHumanAgent)
		default:
			env = env.As(sendapi.MessagingResponse, "")
		}
	}
	return env.ReplyingTo(msg.ReplyToMID).WithMetadata(msg.Metadata), bucket, nil
}

func recipientFor(r fbdomain.Recipient) sendapi.Recipient {
	switch {
	case r.CommentID != "":
		return sendapi.ToComment(r.CommentID)
	case r.PostID != "":
		return sendapi.ToPost(r.PostID)
	}
	return sendapi.ToUser(r.PSID)
}

func optionsFor(options []fbdomain.Option) []sendapi.Option {
	out := make([]sendapi.Option, 0, len(options))
	for _, o := range options {
		out = append(out, sendapi.Option{Title: o.Title, Payload: o.Payload})
	}
	return out
}

func buttonsFor(options []fbdomain.Option) []sendapi.Button {
	out := make([]sendapi.Button, 0, min(len(options), fbdomain.MaxTemplateButtons))
	for _, o := range options {
		if len(out) == fbdomain.MaxTemplateButtons {
			break
		}
		out = append(out, sendapi.Button{Type: "postback", Title: sendapi.TruncateRunes(o.Title, fbdomain.MaxOptionTitleRunes), Payload: o.Payload})
	}
	return out
}

func (s *messagingService) SendAction(ctx context.Context, fbPageID, pageToken, psid string, action fbdomain.SenderAction) error {
	return s.action(ctx, fbPageID, pageToken, sendapi.Action(sendapi.ToUser(psid), string(action)))
}

func (s *messagingService) React(ctx context.Context, fbPageID, pageToken, psid, mid, reaction string) error {
	return s.action(ctx, fbPageID, pageToken, sendapi.React(sendapi.ToUser(psid), mid, reaction))
}

func (s *messagingService) Unreact(ctx context.Context, fbPageID, pageToken, psid, mid string) error {
	return s.action(ctx, fbPageID, pageToken, sendapi.Unreact(sendapi.ToUser(psid), mid))
}

func (s *messagingService) action(ctx context.Context, fbPageID, pageToken string, env sendapi.Envelope) error {
	if err := s.throttle.Allow(bucketText, fbPageID); err != nil {
		return err
	}
	_, err := sendapi.Send(ctx, s.client, "/"+fbPageID+"/messages", pageToken, env)
	return err
}

func (s *messagingService) Upload(ctx context.Context, fbPageID, pageToken string, in fbdomain.UploadInput) (string, error) {
	kind, err := sendapi.AttachmentTypeFor(in.Kind)
	if err != nil {
		return "", err
	}
	if err := s.throttle.Allow(bucketMedia, fbPageID); err != nil {
		return "", err
	}
	reusable := true
	payload := sendapi.AttachmentPayload{URL: in.URL, IsReusable: &reusable}
	message := map[string]any{"attachment": sendapi.Attachment{Type: kind, Payload: payload}}

	var out struct {
		AttachmentID string `json:"attachment_id"`
	}
	req := meta.Request{Method: http.MethodPost, Path: "/" + fbPageID + "/message_attachments", Token: pageToken}
	if in.URL != "" {
		req.Body = map[string]any{"message": message}
	} else {
		payload.URL = ""
		raw, err := json.Marshal(map[string]any{"attachment": sendapi.Attachment{Type: kind, Payload: payload}})
		if err != nil {
			return "", err
		}
		req.Form = url.Values{"message": {string(raw)}}
		req.File = &meta.FilePart{Field: "filedata", FileName: in.FileName, ContentType: in.MIMEType, Data: in.Bytes}
	}
	if err := s.client.Do(ctx, req, &out); err != nil {
		return "", err
	}
	if out.AttachmentID == "" {
		return "", fmt.Errorf("facebook: attachment upload returned no id")
	}
	return out.AttachmentID, nil
}

type profileResponse struct {
	Name       string `json:"name"`
	FirstName  string `json:"first_name"`
	LastName   string `json:"last_name"`
	ProfilePic string `json:"profile_pic"`
}

func (s *messagingService) GetProfile(ctx context.Context, pageToken, psid string) (*fbdomain.ProfileResult, error) {
	q := url.Values{}
	q.Set("fields", "name,first_name,last_name,profile_pic")
	var out profileResponse
	if err := s.client.Do(ctx, meta.Request{Method: http.MethodGet, Path: "/" + psid, Token: pageToken, Query: q}, &out); err != nil {
		return nil, err
	}
	return &fbdomain.ProfileResult{Name: out.Name, FirstName: out.FirstName, LastName: out.LastName, PictureURL: out.ProfilePic}, nil
}

func (s *messagingService) FetchBytes(ctx context.Context, rawURL string) ([]byte, string, error) {
	return s.client.FetchBytes(ctx, rawURL)
}
