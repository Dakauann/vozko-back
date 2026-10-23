package emailinbox

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"net/url"
	"path"
	"strings"

	"vozko/domain/channel"
	"vozko/domain/conversation"
	"vozko/domain/emailsending"
	"vozko/domain/media"
)

const (
	SendCodeAttachmentBlocked  = "email_attachment_blocked"
	SendCodeAttachmentTooLarge = "email_attachment_too_large"
)

func (a *ChannelAdapter) replyAttachment(ctx context.Context, req conversation.SendMediaRequest) (emailsending.Attachment, error) {
	limit, ok := a.caps.MediaLimits[channel.MediaKind(req.Kind)]
	if !ok {
		return emailsending.Attachment{}, fmt.Errorf("%w: email does not carry %q attachments", conversation.ErrCapabilityUnsupported, req.Kind)
	}
	data, contentType, err := a.attachmentBytes(ctx, req, limit.MaxBytes)
	if err != nil {
		return emailsending.Attachment{}, attachmentRefusal(err)
	}
	if !limit.Allows(contentType) {
		return emailsending.Attachment{}, fmt.Errorf("%w: email does not accept %s as %s", conversation.ErrCapabilityUnsupported, contentType, req.Kind)
	}
	attachment := emailsending.Attachment{
		FileName:    emailsending.AttachmentName(attachmentSourceName(req), contentType),
		ContentType: contentType,
		Data:        data,
	}
	if err := emailsending.CheckAttachments([]emailsending.Attachment{attachment}); err != nil {
		return emailsending.Attachment{}, attachmentRefusal(err)
	}
	return attachment, nil
}

func (a *ChannelAdapter) attachmentBytes(ctx context.Context, req conversation.SendMediaRequest, maxBytes int64) ([]byte, string, error) {
	data, contentType := req.Bytes, ""
	switch {
	case len(data) > 0:
		if int64(len(data)) > maxBytes {
			return nil, "", fmt.Errorf("%w: %d bytes over the %d byte budget", emailsending.ErrAttachmentTooLarge, len(data), maxBytes)
		}
	case strings.TrimSpace(req.URL) != "":
		file, err := media.Download(ctx, a.d.Files, req.URL, maxBytes)
		if err != nil {
			return nil, "", err
		}
		data, contentType = file.Data, file.ContentType
	default:
		return nil, "", fmt.Errorf("%w: an email attachment needs its bytes or a url", conversation.ErrCapabilityUnsupported)
	}
	if declared := strings.TrimSpace(req.MIMEType); declared != "" {
		contentType = declared
	}
	if strings.TrimSpace(contentType) == "" {
		contentType = http.DetectContentType(data)
	}
	return data, contentType, nil
}

func attachmentSourceName(req conversation.SendMediaRequest) string {
	if name := strings.TrimSpace(req.FileName); name != "" {
		return name
	}
	parsed, err := url.Parse(strings.TrimSpace(req.URL))
	if err != nil {
		return ""
	}
	name := path.Base(parsed.Path)
	if name == "." || name == "/" {
		return ""
	}
	return name
}

func attachmentRefusal(err error) error {
	switch {
	case errors.Is(err, emailsending.ErrAttachmentBlocked):
		return &conversation.SendRefusal{Code: SendCodeAttachmentBlocked, Err: err}
	case errors.Is(err, emailsending.ErrAttachmentTooLarge):
		return &conversation.SendRefusal{Code: SendCodeAttachmentTooLarge, Err: err}
	case errors.Is(err, media.ErrMediaTooLarge):
		return &conversation.SendRefusal{Code: SendCodeAttachmentTooLarge, Err: fmt.Errorf("%w: %w", emailsending.ErrAttachmentTooLarge, err)}
	}
	return err
}
