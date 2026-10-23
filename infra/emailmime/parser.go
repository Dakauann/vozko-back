package emailmime

import (
	"bytes"
	"encoding/base64"
	"fmt"
	"io"
	"mime"
	"mime/multipart"
	"mime/quotedprintable"
	"net/mail"
	"net/textproto"
	"strings"

	"vozko/domain/emailaddress"
	"vozko/domain/emailinbox"
)

const (
	maxDepth     = 8
	maxParts     = 200
	maxTextBytes = 4 << 20
)

type Parser struct {
	maxAttachmentBytes int64
}

var _ emailinbox.MessageParser = Parser{}

func NewParser() Parser {
	return Parser{maxAttachmentBytes: emailinbox.MaxAttachmentTotalBytes}
}

type collected struct {
	plain       []string
	html        []string
	attachments []emailinbox.Attachment
	parts       int
}

func (p Parser) Parse(raw []byte) (emailinbox.InboundMessage, error) {
	if len(bytes.TrimSpace(raw)) == 0 {
		return emailinbox.InboundMessage{}, fmt.Errorf("%w: empty", emailinbox.ErrUnparseableMessage)
	}
	msg, err := mail.ReadMessage(bytes.NewReader(raw))
	if err != nil {
		return emailinbox.InboundMessage{}, fmt.Errorf("%w: %v", emailinbox.ErrUnparseableMessage, err)
	}
	from, err := fromMailbox(msg.Header)
	if err != nil {
		return emailinbox.InboundMessage{}, err
	}
	out := emailinbox.InboundMessage{
		From:       from,
		To:         recipients(msg.Header),
		Subject:    decodeHeader(msg.Header.Get("Subject")),
		MessageID:  emailinbox.NormalizeMessageID(msg.Header.Get("Message-Id")),
		InReplyTo:  firstID(msg.Header.Get("In-Reply-To")),
		References: idList(msg.Header.Get("References")),
		Automated:  automated(msg.Header),
	}
	if date, err := msg.Header.Date(); err == nil {
		out.Date = date.UTC()
	}

	var c collected
	p.walk(textproto.MIMEHeader(msg.Header), msg.Body, 0, &c)
	text := strings.TrimSpace(strings.Join(c.plain, "\n\n"))
	if text == "" && len(c.html) > 0 {
		text = HTMLToText(strings.Join(c.html, "\n"))
	}
	out.Text, out.QuotedText = emailinbox.StripQuoted(text)
	out.HTML = strings.TrimSpace(strings.Join(c.html, "\n"))
	out.Attachments = c.attachments
	return out, nil
}

func (p Parser) walk(header textproto.MIMEHeader, body io.Reader, depth int, c *collected) {
	c.parts++
	if depth > maxDepth || c.parts > maxParts {
		return
	}
	mediaType, params, err := mime.ParseMediaType(header.Get("Content-Type"))
	if err != nil {
		mediaType, params = "text/plain", map[string]string{}
	}
	if strings.HasPrefix(mediaType, "multipart/") {
		boundary := params["boundary"]
		if boundary == "" {
			return
		}
		reader := multipart.NewReader(body, boundary)
		for {
			part, err := reader.NextRawPart()
			if err != nil {
				return
			}
			p.walk(part.Header, part, depth+1, c)
		}
	}
	disposition, dispParams, _ := mime.ParseMediaType(header.Get("Content-Disposition"))
	name := fileName(dispParams, params)
	isAttachment := disposition == "attachment" || (name != "" && !strings.HasPrefix(mediaType, "text/")) ||
		(!strings.HasPrefix(mediaType, "text/") && mediaType != "message/rfc822")
	if mediaType == "message/rfc822" && disposition != "attachment" {
		isAttachment = true
		if name == "" {
			name = "mensagem.eml"
		}
	}
	if isAttachment {
		c.attachments = append(c.attachments, p.attachment(header, body, mediaType, name, disposition == "inline"))
		return
	}
	data, err := io.ReadAll(io.LimitReader(decodeTransfer(header, body), maxTextBytes))
	if err != nil {
		return
	}
	text := decodeCharset(params["charset"], data)
	switch mediaType {
	case "text/html":
		c.html = append(c.html, text)
	default:
		c.plain = append(c.plain, strings.ReplaceAll(text, "\r\n", "\n"))
	}
}

func (p Parser) attachment(header textproto.MIMEHeader, body io.Reader, mediaType, name string, inline bool) emailinbox.Attachment {
	att := emailinbox.Attachment{FileName: name, ContentType: mediaType, Inline: inline}
	data, err := io.ReadAll(io.LimitReader(decodeTransfer(header, body), p.maxAttachmentBytes+1))
	if err != nil {
		return att
	}
	att.Size = int64(len(data))
	if att.Size > p.maxAttachmentBytes {
		rest, _ := io.Copy(io.Discard, body)
		att.Size += rest
		return att
	}
	att.Data = data
	return att
}

func decodeTransfer(header textproto.MIMEHeader, body io.Reader) io.Reader {
	switch strings.ToLower(strings.TrimSpace(header.Get("Content-Transfer-Encoding"))) {
	case "base64":
		return base64.NewDecoder(base64.StdEncoding, &whitespaceStripper{r: body})
	case "quoted-printable":
		return quotedprintable.NewReader(body)
	}
	return body
}

type whitespaceStripper struct {
	r io.Reader
}

func (w *whitespaceStripper) Read(p []byte) (int, error) {
	for {
		n, err := w.r.Read(p)
		kept := 0
		for _, b := range p[:n] {
			if b != '\r' && b != '\n' && b != ' ' && b != '\t' {
				p[kept] = b
				kept++
			}
		}
		if kept > 0 || err != nil {
			return kept, err
		}
	}
}

func fileName(disposition, content map[string]string) string {
	for _, candidate := range []string{disposition["filename"], content["name"]} {
		if name := strings.TrimSpace(decodeHeader(candidate)); name != "" {
			return sanitizeFileName(name)
		}
	}
	return ""
}

func sanitizeFileName(name string) string {
	name = strings.NewReplacer("/", "_", "\\", "_", "\x00", "").Replace(name)
	if runes := []rune(name); len(runes) > 200 {
		name = string(runes[:200])
	}
	return name
}

func fromMailbox(h mail.Header) (emailinbox.Mailbox, error) {
	raw := h.Get("From")
	if strings.TrimSpace(raw) == "" {
		return emailinbox.Mailbox{}, emailinbox.ErrNoSender
	}
	parsed, err := addressParser.Parse(raw)
	if err != nil {
		return emailinbox.Mailbox{}, fmt.Errorf("%w: %v", emailinbox.ErrNoSender, err)
	}
	address, err := emailaddress.Normalize(parsed.Address)
	if err != nil {
		return emailinbox.Mailbox{}, fmt.Errorf("%w: %v", emailinbox.ErrNoSender, err)
	}
	return emailinbox.Mailbox{Name: strings.TrimSpace(parsed.Name), Address: address}, nil
}

func recipients(h mail.Header) []emailaddress.Address {
	var out []emailaddress.Address
	seen := map[emailaddress.Address]struct{}{}
	for _, field := range []string{"To", "Cc", "Delivered-To", "X-Original-To"} {
		list, err := addressParser.ParseList(h.Get(field))
		if err != nil {
			continue
		}
		for _, a := range list {
			address, err := emailaddress.Normalize(a.Address)
			if err != nil {
				continue
			}
			if _, dup := seen[address]; dup {
				continue
			}
			seen[address] = struct{}{}
			out = append(out, address)
		}
	}
	return out
}

func firstID(raw string) string {
	ids := idList(raw)
	if len(ids) == 0 {
		return ""
	}
	return ids[0]
}

func idList(raw string) []string {
	var out []string
	rest := raw
	for {
		start := strings.IndexByte(rest, '<')
		if start < 0 {
			break
		}
		end := strings.IndexByte(rest[start:], '>')
		if end < 0 {
			break
		}
		if id := emailinbox.NormalizeMessageID(rest[start : start+end+1]); id != "" {
			out = append(out, id)
		}
		rest = rest[start+end+1:]
	}
	if len(out) == 0 {
		for _, field := range strings.Fields(raw) {
			if id := emailinbox.NormalizeMessageID(field); id != "" && strings.Contains(id, "@") {
				out = append(out, id)
			}
		}
	}
	return out
}

func automated(h mail.Header) bool {
	if v := strings.ToLower(strings.TrimSpace(h.Get("Auto-Submitted"))); v != "" && v != "no" {
		return true
	}
	switch strings.ToLower(strings.TrimSpace(h.Get("Precedence"))) {
	case "auto_reply", "bulk", "junk", "list":
		return true
	}
	for _, header := range []string{"X-Autoreply", "X-Autorespond"} {
		if v := strings.ToLower(strings.TrimSpace(h.Get(header))); v != "" && v != "no" {
			return true
		}
	}
	return h.Get("List-Id") != "" || h.Get("List-Unsubscribe") != ""
}
