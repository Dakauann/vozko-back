package mercadolivre

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"mime/multipart"
	"net/http"
	"net/textproto"
	"net/url"
	"strings"

	mldomain "vozko/domain/mercadolivre"
)

type AttachmentService struct {
	c    *Client
	site string
}

var _ mldomain.AttachmentService = (*AttachmentService)(nil)

func NewAttachmentService(c *Client) (*AttachmentService, error) {
	if c == nil {
		return nil, errors.New("mercadolivre: attachment service requires a client")
	}
	return &AttachmentService{c: c, site: mldomain.SiteMLB}, nil
}

func (s *AttachmentService) query() url.Values {
	q := url.Values{}
	q.Set("tag", tagPostSale)
	q.Set("site_id", s.site)
	return q
}

func (s *AttachmentService) Upload(ctx context.Context, creds mldomain.Credentials, upload mldomain.AttachmentUpload) (string, error) {
	if err := mldomain.ValidateAttachment(upload.FileName, upload.ContentType, int64(len(upload.Data))); err != nil {
		return "", err
	}
	var buf bytes.Buffer
	writer := multipart.NewWriter(&buf)
	header := textproto.MIMEHeader{}
	header.Set("Content-Disposition", fmt.Sprintf(`form-data; name="file"; filename=%q`, strings.TrimSpace(upload.FileName)))
	header.Set("Content-Type", upload.ContentType)
	part, err := writer.CreatePart(header)
	if err != nil {
		return "", err
	}
	if _, err := part.Write(upload.Data); err != nil {
		return "", err
	}
	if err := writer.Close(); err != nil {
		return "", err
	}
	resp, err := s.c.do(ctx, request{
		method:      http.MethodPost,
		path:        "/messages/attachments",
		query:       s.query(),
		family:      FamilyMessagesWrite,
		creds:       creds,
		body:        buf.Bytes(),
		contentType: writer.FormDataContentType(),
	})
	if err != nil {
		return "", err
	}
	var out struct {
		ID string `json:"id"`
	}
	if err := json.Unmarshal(resp.body, &out); err != nil || !validAttachmentID(out.ID) {
		return "", markAcceptedButUnreadable(resp, &Error{HTTPStatus: resp.status, Code: CodeDecode, Message: "upload answer carries no attachment id", Err: err})
	}
	return out.ID, nil
}

func (s *AttachmentService) Download(ctx context.Context, creds mldomain.Credentials, attachmentID string) (*mldomain.AttachmentDownload, error) {
	if !validAttachmentID(attachmentID) {
		return nil, fmt.Errorf("%w: attachment id %q", mldomain.ErrMediaUnsupported, attachmentID)
	}
	resp, err := s.c.do(ctx, request{
		method:   http.MethodGet,
		path:     "/messages/attachments/" + pathEscape(attachmentID),
		query:    s.query(),
		family:   FamilyMessagesRead,
		creds:    creds,
		maxBytes: mldomain.MaxAttachmentBytes,
		headers:  map[string]string{"Accept": "*/*"},
	})
	if err != nil {
		return nil, err
	}
	return &mldomain.AttachmentDownload{Data: resp.body, ContentType: resp.header.Get("Content-Type")}, nil
}
