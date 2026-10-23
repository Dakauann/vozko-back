package emailapi

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"net/textproto"
	"net/url"
	"sort"
	"strings"
	"unicode/utf8"

	"vozko/domain/emailaddress"
	es "vozko/domain/emailsending"
)

const (
	MaxRecipients        = 50
	MaxReplyTo           = 5
	MaxTags              = 10
	MaxTagPartLength     = 256
	MaxHeaders           = 20
	MaxHeaderValueBytes  = 998
	MaxSubjectBytes      = 998
	MaxIdempotencyKeyLen = 256
	MaxAttachments       = 20
)

type Origin string

const (
	OriginAPI    Origin = "api"
	OriginSystem Origin = "system"
)

func (o Origin) Valid() bool { return o == OriginAPI || o == OriginSystem }

type Role string

const (
	RoleTo  Role = "to"
	RoleCc  Role = "cc"
	RoleBcc Role = "bcc"
)

type Recipient struct {
	Position int
	Role     Role
	Address  emailaddress.Address
}

type Tag struct {
	Name  string
	Value string
}

type Content struct {
	Subject   string
	HTML      string
	Text      string
	DesignID  string
	Variables map[string]string
}

func (c Content) UsesDesign() bool { return strings.TrimSpace(c.DesignID) != "" }

type AttachmentSource struct {
	FileName    string
	ContentType string
	Data        []byte
	URL         string
}

type Request struct {
	WorkspaceID    string
	Origin         Origin
	APIKeyID       string
	From           string
	To             []string
	Cc             []string
	Bcc            []string
	ReplyTo        []string
	Content        Content
	Headers        map[string]string
	Tags           []Tag
	Attachments    []AttachmentSource
	IdempotencyKey string
}

type Draft struct {
	WorkspaceID    string
	Origin         Origin
	APIKeyID       string
	From           string
	Recipients     []Recipient
	ReplyTo        []emailaddress.Address
	Content        Content
	Headers        []es.Header
	Tags           []Tag
	Attachments    []AttachmentSource
	IdempotencyKey string
}

func (d Draft) TagMap() map[string]string {
	out := make(map[string]string, len(d.Tags))
	for _, t := range d.Tags {
		out[t.Name] = t.Value
	}
	return out
}

func (d Draft) Addresses() []emailaddress.Address {
	out := make([]emailaddress.Address, len(d.Recipients))
	for i, r := range d.Recipients {
		out[i] = r.Address
	}
	return out
}

func (r Request) Validate() (Draft, error) {
	if strings.TrimSpace(r.WorkspaceID) == "" {
		return Draft{}, ErrWorkspaceRequired
	}
	if !r.Origin.Valid() {
		return Draft{}, fmt.Errorf("%w: %q", ErrOriginInvalid, r.Origin)
	}
	key, err := ValidateIdempotencyKey(r.IdempotencyKey)
	if err != nil {
		return Draft{}, err
	}
	from := strings.TrimSpace(r.From)
	if from == "" {
		return Draft{}, ErrFromRequired
	}
	recipients, err := normalizeRecipients(r.To, r.Cc, r.Bcc)
	if err != nil {
		return Draft{}, err
	}
	replyTo, err := normalizeReplyTo(r.ReplyTo)
	if err != nil {
		return Draft{}, err
	}
	content, err := validateContent(r.Content)
	if err != nil {
		return Draft{}, err
	}
	headers, err := ValidateHeaders(r.Headers)
	if err != nil {
		return Draft{}, err
	}
	tags, err := ValidateTags(r.Tags)
	if err != nil {
		return Draft{}, err
	}
	if err := validateAttachments(r.Attachments); err != nil {
		return Draft{}, err
	}
	return Draft{
		WorkspaceID:    strings.TrimSpace(r.WorkspaceID),
		Origin:         r.Origin,
		APIKeyID:       r.APIKeyID,
		From:           from,
		Recipients:     recipients,
		ReplyTo:        replyTo,
		Content:        content,
		Headers:        headers,
		Tags:           tags,
		Attachments:    r.Attachments,
		IdempotencyKey: key,
	}, nil
}

func ValidateIdempotencyKey(raw string) (string, error) {
	key := strings.TrimSpace(raw)
	if key == "" {
		return "", ErrIdempotencyKeyRequired
	}
	if len(key) > MaxIdempotencyKeyLen {
		return "", ErrIdempotencyKeyInvalid
	}
	for i := 0; i < len(key); i++ {
		if key[i] < 0x20 || key[i] > 0x7e {
			return "", ErrIdempotencyKeyInvalid
		}
	}
	return key, nil
}

func normalizeRecipients(to, cc, bcc []string) ([]Recipient, error) {
	seen := map[emailaddress.Address]bool{}
	var out []Recipient
	for _, group := range []struct {
		role Role
		list []string
	}{{RoleTo, to}, {RoleCc, cc}, {RoleBcc, bcc}} {
		for _, raw := range group.list {
			addr, err := emailaddress.Normalize(raw)
			if err != nil {
				return nil, fmt.Errorf("%w: %q", ErrAddressInvalid, raw)
			}
			if seen[addr] {
				continue
			}
			seen[addr] = true
			out = append(out, Recipient{Position: len(out), Role: group.role, Address: addr})
		}
	}
	if len(out) == 0 || len(out) > MaxRecipients || len(to) == 0 {
		return nil, ErrRecipientsInvalid
	}
	return out, nil
}

func normalizeReplyTo(raw []string) ([]emailaddress.Address, error) {
	if len(raw) > MaxReplyTo {
		return nil, ErrReplyToInvalid
	}
	out := make([]emailaddress.Address, 0, len(raw))
	for _, r := range raw {
		addr, err := emailaddress.Normalize(r)
		if err != nil {
			return nil, fmt.Errorf("%w: %q", ErrReplyToInvalid, r)
		}
		out = append(out, addr)
	}
	return out, nil
}

func validateContent(c Content) (Content, error) {
	subject := strings.TrimSpace(c.Subject)
	if subject == "" || len(subject) > MaxSubjectBytes || strings.ContainsAny(subject, "\r\n\x00") || !utf8.ValidString(subject) {
		return Content{}, ErrSubjectInvalid
	}
	c.Subject = subject
	c.DesignID = strings.TrimSpace(c.DesignID)
	hasBody := c.HTML != "" || c.Text != ""
	switch {
	case c.UsesDesign() && hasBody:
		return Content{}, ErrBodyInvalid
	case !c.UsesDesign() && !hasBody:
		return Content{}, ErrBodyInvalid
	case !c.UsesDesign() && len(c.Variables) > 0:
		return Content{}, ErrBodyInvalid
	case !utf8.ValidString(c.HTML) || !utf8.ValidString(c.Text):
		return Content{}, ErrBodyInvalid
	}
	return c, nil
}

var exactHeaders = map[string]struct{}{
	"List-Unsubscribe":      {},
	"List-Unsubscribe-Post": {},
	"List-Id":               {},
	"Auto-Submitted":        {},
	"Precedence":            {},
	"Entity-Ref-Id":         {},
}

func HeaderAllowed(name string) bool {
	canonical := textproto.CanonicalMIMEHeaderKey(strings.TrimSpace(name))
	if _, ok := exactHeaders[canonical]; ok {
		return true
	}
	return strings.HasPrefix(canonical, "X-") && !strings.HasPrefix(canonical, "X-Ses-") && len(canonical) > 2
}

func ValidateHeaders(raw map[string]string) ([]es.Header, error) {
	if len(raw) > MaxHeaders {
		return nil, fmt.Errorf("%w: at most %d headers", ErrHeaderNotAllowed, MaxHeaders)
	}
	out := make([]es.Header, 0, len(raw))
	for name, value := range raw {
		canonical := textproto.CanonicalMIMEHeaderKey(strings.TrimSpace(name))
		if !validHeaderName(canonical) || !HeaderAllowed(canonical) {
			return nil, fmt.Errorf("%w: %q", ErrHeaderNotAllowed, name)
		}
		if strings.ContainsAny(value, "\r\n\x00") || len(value) > MaxHeaderValueBytes || !utf8.ValidString(value) {
			return nil, fmt.Errorf("%w: value of %q", ErrHeaderNotAllowed, name)
		}
		out = append(out, es.Header{Name: canonical, Value: value})
	}
	sort.Slice(out, func(i, j int) bool { return out[i].Name < out[j].Name })
	for i := 1; i < len(out); i++ {
		if out[i].Name == out[i-1].Name {
			return nil, fmt.Errorf("%w: %q repeated", ErrHeaderNotAllowed, out[i].Name)
		}
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

func ValidTagPart(s string) bool {
	if s == "" || len(s) > MaxTagPartLength {
		return false
	}
	for _, r := range s {
		if !(r >= 'a' && r <= 'z') && !(r >= 'A' && r <= 'Z') && !(r >= '0' && r <= '9') && r != '_' && r != '-' {
			return false
		}
	}
	return true
}

func ValidateTags(raw []Tag) ([]Tag, error) {
	if len(raw) > MaxTags {
		return nil, ErrTagsInvalid
	}
	out := append([]Tag(nil), raw...)
	for _, t := range out {
		if !ValidTagPart(t.Name) || !ValidTagPart(t.Value) {
			return nil, fmt.Errorf("%w: %q", ErrTagsInvalid, t.Name)
		}
	}
	sort.Slice(out, func(i, j int) bool { return out[i].Name < out[j].Name })
	for i := 1; i < len(out); i++ {
		if out[i].Name == out[i-1].Name {
			return nil, fmt.Errorf("%w: %q repeated", ErrTagsInvalid, out[i].Name)
		}
	}
	return out, nil
}

func validateAttachments(list []AttachmentSource) error {
	if len(list) > MaxAttachments {
		return fmt.Errorf("%w: at most %d attachments", ErrAttachmentInvalid, MaxAttachments)
	}
	for _, a := range list {
		hasData, hasURL := len(a.Data) > 0, strings.TrimSpace(a.URL) != ""
		if strings.TrimSpace(a.FileName) == "" || hasData == hasURL {
			return fmt.Errorf("%w: %q", ErrAttachmentInvalid, a.FileName)
		}
		if hasURL {
			parsed, err := url.Parse(strings.TrimSpace(a.URL))
			if err != nil || parsed.Scheme != "https" || parsed.Host == "" || parsed.User != nil {
				return fmt.Errorf("%w: path of %q", ErrAttachmentInvalid, a.FileName)
			}
		}
	}
	return nil
}

func (d Draft) Fingerprint() string {
	type recipient struct {
		Role    Role
		Address emailaddress.Address
	}
	recipients := make([]recipient, len(d.Recipients))
	for i, r := range d.Recipients {
		recipients[i] = recipient{Role: r.Role, Address: r.Address}
	}
	sort.Slice(recipients, func(i, j int) bool {
		if recipients[i].Role != recipients[j].Role {
			return recipients[i].Role < recipients[j].Role
		}
		return recipients[i].Address < recipients[j].Address
	})
	type attachment struct {
		Name, Type, URL, Data string
	}
	attachments := make([]attachment, len(d.Attachments))
	for i, a := range d.Attachments {
		sum := sha256.Sum256(a.Data)
		attachments[i] = attachment{Name: a.FileName, Type: a.ContentType, URL: strings.TrimSpace(a.URL), Data: hex.EncodeToString(sum[:])}
	}
	canonical, _ := json.Marshal(struct {
		From        string
		Recipients  []recipient
		ReplyTo     []emailaddress.Address
		Content     Content
		Headers     []es.Header
		Tags        []Tag
		Attachments []attachment
	}{d.From, recipients, d.ReplyTo, d.Content, d.Headers, d.Tags, attachments})
	sum := sha256.Sum256(canonical)
	return hex.EncodeToString(sum[:])
}
