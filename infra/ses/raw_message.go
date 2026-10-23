package ses

import (
	"bytes"
	"encoding/base64"
	"errors"
	"fmt"
	"mime"
	"mime/multipart"
	"mime/quotedprintable"
	"net/mail"
	"net/textproto"
	"sort"
	"strings"
	"time"
	"unicode/utf8"

	"github.com/aws/aws-sdk-go-v2/aws"
	"github.com/aws/aws-sdk-go-v2/service/sesv2/types"

	"vozko/domain/emailaddress"
	"vozko/domain/emailsending"
)

var errUnsafeMessage = errors.New("ses: unsafe message content")

const (
	maxTagLength      = 256
	maxHeaderLineSize = 900
	base64LineLength  = 76
	textCharset       = "UTF-8"
)

var reservedHeaders = map[string]struct{}{
	"From":                      {},
	"Sender":                    {},
	"To":                        {},
	"Cc":                        {},
	"Bcc":                       {},
	"Reply-To":                  {},
	"Subject":                   {},
	"Date":                      {},
	"Message-Id":                {},
	"Mime-Version":              {},
	"Content-Type":              {},
	"Content-Transfer-Encoding": {},
	"Return-Path":               {},
	"X-Ses-Tenant":              {},
	"X-Ses-Configuration-Set":   {},
	"X-Ses-Message-Tags":        {},
}

func buildRawMessage(m emailsending.OutboundMessage, messageID string, at time.Time) ([]byte, error) {
	if err := emailsending.CheckAttachments(m.Attachments); err != nil {
		return nil, err
	}
	if m.Text == "" && m.HTML == "" {
		return nil, fmt.Errorf("%w: empty body", errUnsafeMessage)
	}
	if strings.ContainsRune(m.Text, 0) || strings.ContainsRune(m.HTML, 0) {
		return nil, fmt.Errorf("%w: nul byte in body", errUnsafeMessage)
	}
	if !utf8.ValidString(m.Text) || !utf8.ValidString(m.HTML) || !utf8.ValidString(m.Subject) || !utf8.ValidString(m.From.Name) {
		return nil, fmt.Errorf("%w: invalid utf-8", errUnsafeMessage)
	}
	if unsafeText(m.Subject) || strings.TrimSpace(m.Subject) == "" {
		return nil, fmt.Errorf("%w: subject", errUnsafeMessage)
	}
	if unsafeText(m.From.Name) {
		return nil, fmt.Errorf("%w: display name", errUnsafeMessage)
	}
	from, err := safeAddress(m.From.Address)
	if err != nil {
		return nil, err
	}
	to, err := safeAddress(m.To)
	if err != nil {
		return nil, err
	}
	replyTo := make([]string, 0, len(m.ReplyTo))
	for _, a := range m.ReplyTo {
		addr, err := safeAddress(a)
		if err != nil {
			return nil, err
		}
		replyTo = append(replyTo, (&mail.Address{Address: addr}).String())
	}
	extra, err := customHeaders(m.Headers)
	if err != nil {
		return nil, err
	}

	var buf bytes.Buffer
	writeHeader(&buf, "From", (&mail.Address{Name: m.From.Name, Address: from}).String())
	writeHeader(&buf, "To", (&mail.Address{Address: to}).String())
	if len(replyTo) > 0 {
		writeHeader(&buf, "Reply-To", strings.Join(replyTo, ", "))
	}
	writeHeader(&buf, "Subject", encodeHeaderText(m.Subject))
	writeHeader(&buf, "Date", at.Format(time.RFC1123Z))
	writeHeader(&buf, "Message-ID", "<"+messageID+"@"+emailaddress.Address(from).Domain()+">")
	writeHeader(&buf, "MIME-Version", "1.0")
	for _, h := range extra {
		writeHeader(&buf, h.Name, h.Value)
	}

	body, err := messageBody(m)
	if err != nil {
		return nil, err
	}
	for _, name := range []string{"Content-Type", "Content-Transfer-Encoding"} {
		if value := body.header.Get(name); value != "" {
			writeHeader(&buf, name, value)
		}
	}
	buf.WriteString("\r\n")
	buf.Write(body.content)
	if size := int64(buf.Len()); size > emailsending.MaxDeliverableMessageBytes {
		return nil, fmt.Errorf("%w: %d bytes over the %d deliverable bytes", emailsending.ErrMessageTooLarge, size, emailsending.MaxDeliverableMessageBytes)
	}
	return buf.Bytes(), nil
}

type mimePart struct {
	header  textproto.MIMEHeader
	content []byte
}

func messageBody(m emailsending.OutboundMessage) (mimePart, error) {
	text, err := textBody(m)
	if err != nil || len(m.Attachments) == 0 {
		return text, err
	}
	parts := make([]mimePart, 0, len(m.Attachments)+1)
	parts = append(parts, text)
	for _, a := range m.Attachments {
		part, err := attachmentPart(a)
		if err != nil {
			return mimePart{}, err
		}
		parts = append(parts, part)
	}
	return multipartOf("multipart/mixed", parts)
}

func textBody(m emailsending.OutboundMessage) (mimePart, error) {
	if m.Text == "" || m.HTML == "" {
		body, contentType := m.Text, "text/plain"
		if body == "" {
			body, contentType = m.HTML, "text/html"
		}
		return quotedPrintablePart(contentType, body)
	}
	parts := make([]mimePart, 0, 2)
	for _, part := range []struct{ contentType, content string }{
		{"text/plain", m.Text},
		{"text/html", m.HTML},
	} {
		p, err := quotedPrintablePart(part.contentType, part.content)
		if err != nil {
			return mimePart{}, err
		}
		parts = append(parts, p)
	}
	return multipartOf("multipart/alternative", parts)
}

func quotedPrintablePart(contentType, content string) (mimePart, error) {
	var body bytes.Buffer
	if err := writeQuotedPrintable(&body, content); err != nil {
		return mimePart{}, err
	}
	return mimePart{
		header: textproto.MIMEHeader{
			"Content-Type":              {contentType + "; charset=" + textCharset},
			"Content-Transfer-Encoding": {"quoted-printable"},
		},
		content: body.Bytes(),
	}, nil
}

func attachmentPart(a emailsending.Attachment) (mimePart, error) {
	mediaType, params, err := mime.ParseMediaType(a.ContentType)
	if err != nil {
		return mimePart{}, fmt.Errorf("%w: type of %q", emailsending.ErrAttachmentInvalid, a.FileName)
	}
	name := strings.TrimSpace(a.FileName)
	params["name"] = name
	contentType := mime.FormatMediaType(mediaType, params)
	disposition := mime.FormatMediaType("attachment", map[string]string{"filename": name})
	if contentType == "" || disposition == "" {
		return mimePart{}, fmt.Errorf("%w: %q", emailsending.ErrAttachmentInvalid, a.FileName)
	}
	return mimePart{
		header: textproto.MIMEHeader{
			"Content-Type":              {contentType},
			"Content-Disposition":       {disposition},
			"Content-Transfer-Encoding": {"base64"},
		},
		content: wrappedBase64(a.Data),
	}, nil
}

func wrappedBase64(data []byte) []byte {
	encoded := base64.StdEncoding.EncodeToString(data)
	out := make([]byte, 0, emailsending.EncodedAttachmentBytes(int64(len(data))))
	for len(encoded) > 0 {
		line := min(len(encoded), base64LineLength)
		out = append(out, encoded[:line]...)
		out = append(out, '\r', '\n')
		encoded = encoded[line:]
	}
	return out
}

func multipartOf(mediaType string, parts []mimePart) (mimePart, error) {
	var body bytes.Buffer
	mp := multipart.NewWriter(&body)
	for _, part := range parts {
		w, err := mp.CreatePart(part.header)
		if err != nil {
			return mimePart{}, err
		}
		if _, err := w.Write(part.content); err != nil {
			return mimePart{}, err
		}
	}
	if err := mp.Close(); err != nil {
		return mimePart{}, err
	}
	return mimePart{
		header:  textproto.MIMEHeader{"Content-Type": {mime.FormatMediaType(mediaType, map[string]string{"boundary": mp.Boundary()})}},
		content: body.Bytes(),
	}, nil
}

type writer interface {
	Write(p []byte) (int, error)
}

func writeQuotedPrintable(w writer, content string) error {
	qp := quotedprintable.NewWriter(w)
	if _, err := qp.Write([]byte(strings.ReplaceAll(strings.ReplaceAll(content, "\r\n", "\n"), "\n", "\r\n"))); err != nil {
		return err
	}
	return qp.Close()
}

func writeHeader(buf *bytes.Buffer, name, value string) {
	buf.WriteString(name)
	buf.WriteString(": ")
	buf.WriteString(value)
	buf.WriteString("\r\n")
}

func unsafeText(s string) bool {
	return strings.ContainsAny(s, "\r\n\x00")
}

func safeAddress(a emailaddress.Address) (string, error) {
	normalized, err := emailaddress.Normalize(string(a))
	if err != nil || string(normalized) != string(a) {
		return "", fmt.Errorf("%w: address %q", errUnsafeMessage, string(a))
	}
	return string(normalized), nil
}

func encodeHeaderText(s string) string {
	if isPrintableASCII(s) && len(s) <= maxHeaderLineSize {
		return s
	}
	encoded := mime.QEncoding.Encode("utf-8", s)
	return strings.ReplaceAll(encoded, "?= =?", "?=\r\n =?")
}

func isPrintableASCII(s string) bool {
	for i := 0; i < len(s); i++ {
		if s[i] < 0x20 || s[i] > 0x7e {
			return false
		}
	}
	return true
}

func customHeaders(headers []emailsending.Header) ([]emailsending.Header, error) {
	out := make([]emailsending.Header, 0, len(headers))
	for _, h := range headers {
		name := textproto.CanonicalMIMEHeaderKey(strings.TrimSpace(h.Name))
		if !validHeaderName(h.Name) {
			return nil, fmt.Errorf("%w: header name %q", errUnsafeMessage, h.Name)
		}
		if _, reserved := reservedHeaders[name]; reserved {
			return nil, fmt.Errorf("%w: reserved header %q", errUnsafeMessage, h.Name)
		}
		if unsafeText(h.Value) || !utf8.ValidString(h.Value) {
			return nil, fmt.Errorf("%w: header value of %q", errUnsafeMessage, h.Name)
		}
		out = append(out, emailsending.Header{Name: h.Name, Value: encodeHeaderText(h.Value)})
	}
	return out, nil
}

func validHeaderName(name string) bool {
	if name == "" {
		return false
	}
	for i := 0; i < len(name); i++ {
		c := name[i]
		if c <= 0x20 || c >= 0x7f || c == ':' {
			return false
		}
	}
	return true
}

func sanitizeTags(tags map[string]string) []types.MessageTag {
	keys := make([]string, 0, len(tags))
	clean := make(map[string]string, len(tags))
	for k, v := range tags {
		name, value := sanitizeTagPart(k), sanitizeTagPart(v)
		if name == "" || value == "" {
			continue
		}
		if _, dup := clean[name]; !dup {
			keys = append(keys, name)
		}
		clean[name] = value
	}
	sort.Strings(keys)
	out := make([]types.MessageTag, 0, len(keys))
	for _, k := range keys {
		out = append(out, types.MessageTag{Name: aws.String(k), Value: aws.String(clean[k])})
	}
	return out
}

func sanitizeTagPart(s string) string {
	var b strings.Builder
	for _, r := range s {
		if b.Len() >= maxTagLength {
			break
		}
		if (r >= 'a' && r <= 'z') || (r >= 'A' && r <= 'Z') || (r >= '0' && r <= '9') || r == '_' || r == '-' {
			b.WriteRune(r)
			continue
		}
		b.WriteByte('_')
	}
	return b.String()
}
