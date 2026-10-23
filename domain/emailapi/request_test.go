package emailapi

import (
	"errors"
	"fmt"
	"strings"
	"testing"

	"vozko/domain/emailaddress"
)

func validRequest() Request {
	return Request{
		WorkspaceID:    "ws-1",
		Origin:         OriginAPI,
		From:           "sender-1",
		To:             []string{"Ana@Example.com"},
		Content:        Content{Subject: "Seu código", HTML: "<p>123</p>"},
		IdempotencyKey: "reset-42",
	}
}

func TestValidateNormalizesRecipientsAndKeepsTheFirstRoleOfARepeatedAddress(t *testing.T) {
	r := validRequest()
	r.To = []string{"ana@example.com", " BIA@example.com "}
	r.Cc = []string{"ana@example.com", "caio@example.com"}
	r.Bcc = []string{"dani@example.com"}
	d, err := r.Validate()
	if err != nil {
		t.Fatal(err)
	}
	want := []Recipient{
		{Position: 0, Role: RoleTo, Address: "ana@example.com"},
		{Position: 1, Role: RoleTo, Address: "bia@example.com"},
		{Position: 2, Role: RoleCc, Address: "caio@example.com"},
		{Position: 3, Role: RoleBcc, Address: "dani@example.com"},
	}
	if len(d.Recipients) != len(want) {
		t.Fatalf("recipients = %+v", d.Recipients)
	}
	for i := range want {
		if d.Recipients[i] != want[i] {
			t.Fatalf("recipient %d = %+v, want %+v", i, d.Recipients[i], want[i])
		}
	}
}

func TestValidateRefusals(t *testing.T) {
	many := make([]string, MaxRecipients+1)
	for i := range many {
		many[i] = fmt.Sprintf("p%d@example.com", i)
	}
	cases := []struct {
		name   string
		mutate func(*Request)
		want   error
	}{
		{"no key", func(r *Request) { r.IdempotencyKey = "  " }, ErrIdempotencyKeyRequired},
		{"long key", func(r *Request) { r.IdempotencyKey = strings.Repeat("k", 257) }, ErrIdempotencyKeyInvalid},
		{"key with control", func(r *Request) { r.IdempotencyKey = "a\nb" }, ErrIdempotencyKeyInvalid},
		{"no from", func(r *Request) { r.From = "" }, ErrFromRequired},
		{"no recipients", func(r *Request) { r.To = nil }, ErrRecipientsInvalid},
		{"too many", func(r *Request) { r.To = many }, ErrRecipientsInvalid},
		{"bad address", func(r *Request) { r.Cc = []string{"nope"} }, ErrAddressInvalid},
		{"bad reply to", func(r *Request) { r.ReplyTo = []string{"x"} }, ErrReplyToInvalid},
		{"too many reply to", func(r *Request) {
			r.ReplyTo = []string{"a@x.com", "b@x.com", "c@x.com", "d@x.com", "e@x.com", "f@x.com"}
		}, ErrReplyToInvalid},
		{"no subject", func(r *Request) { r.Content.Subject = " " }, ErrSubjectInvalid},
		{"subject with break", func(r *Request) { r.Content.Subject = "a\r\nBcc: x" }, ErrSubjectInvalid},
		{"no body", func(r *Request) { r.Content.HTML = "" }, ErrBodyInvalid},
		{"body and design", func(r *Request) { r.Content.DesignID = "d-1" }, ErrBodyInvalid},
		{"variables without design", func(r *Request) { r.Content.Variables = map[string]string{"a": "b"} }, ErrBodyInvalid},
		{"reserved header", func(r *Request) { r.Headers = map[string]string{"Reply-To": "x@y.com"} }, ErrHeaderNotAllowed},
		{"ses header", func(r *Request) { r.Headers = map[string]string{"X-SES-Tenant": "x"} }, ErrHeaderNotAllowed},
		{"header injection", func(r *Request) { r.Headers = map[string]string{"X-Ref": "a\r\nBcc: x"} }, ErrHeaderNotAllowed},
		{"bad tag", func(r *Request) { r.Tags = []Tag{{Name: "a b", Value: "x"}} }, ErrTagsInvalid},
		{"repeated tag", func(r *Request) { r.Tags = []Tag{{Name: "a", Value: "x"}, {Name: "a", Value: "y"}} }, ErrTagsInvalid},
		{"attachment without source", func(r *Request) { r.Attachments = []AttachmentSource{{FileName: "a.pdf"}} }, ErrAttachmentInvalid},
		{"attachment with both sources", func(r *Request) {
			r.Attachments = []AttachmentSource{{FileName: "a.pdf", Data: []byte("x"), URL: "https://x.com/a.pdf"}}
		}, ErrAttachmentInvalid},
		{"attachment over http", func(r *Request) { r.Attachments = []AttachmentSource{{FileName: "a.pdf", URL: "http://x.com/a.pdf"}} }, ErrAttachmentInvalid},
		{"unknown origin", func(r *Request) { r.Origin = "mars" }, ErrOriginInvalid},
		{"no workspace", func(r *Request) { r.WorkspaceID = "" }, ErrWorkspaceRequired},
	}
	for _, c := range cases {
		r := validRequest()
		c.mutate(&r)
		if _, err := r.Validate(); !errors.Is(err, c.want) {
			t.Fatalf("%s: err = %v, want %v", c.name, err, c.want)
		}
	}
}

func TestValidateAcceptsAllowedHeadersTagsAndAttachments(t *testing.T) {
	r := validRequest()
	r.Headers = map[string]string{"x-entity-ref-id": "123", "List-Unsubscribe": "<https://x.com/u>", "Auto-Submitted": "auto-generated"}
	r.Tags = []Tag{{Name: "kind", Value: "reset"}, {Name: "app", Value: "web_1"}}
	r.ReplyTo = []string{"Suporte@Example.com"}
	r.Attachments = []AttachmentSource{{FileName: "nota.pdf", Data: []byte("%PDF")}, {FileName: "logo.png", URL: "https://cdn.example.com/logo.png"}}
	d, err := r.Validate()
	if err != nil {
		t.Fatal(err)
	}
	if len(d.Headers) != 3 || d.Headers[0].Name != "Auto-Submitted" || d.Headers[2].Name != "X-Entity-Ref-Id" {
		t.Fatalf("headers = %+v", d.Headers)
	}
	if d.Tags[0].Name != "app" || d.TagMap()["kind"] != "reset" {
		t.Fatalf("tags = %+v", d.Tags)
	}
	if len(d.ReplyTo) != 1 || d.ReplyTo[0] != emailaddress.Address("suporte@example.com") {
		t.Fatalf("reply to = %+v", d.ReplyTo)
	}
}

func TestFingerprintIgnoresOrderAndCaseButNotContent(t *testing.T) {
	a := validRequest()
	a.To = []string{"ana@example.com", "bia@example.com"}
	a.Tags = []Tag{{Name: "a", Value: "1"}, {Name: "b", Value: "2"}}
	b := a
	b.To = []string{"BIA@example.com", "ana@example.com"}
	b.Tags = []Tag{{Name: "b", Value: "2"}, {Name: "a", Value: "1"}}
	da, _ := a.Validate()
	db, _ := b.Validate()
	if da.Fingerprint() != db.Fingerprint() {
		t.Fatal("reordering recipients or tags changed the fingerprint")
	}
	c := a
	c.Content.HTML = "<p>456</p>"
	dc, _ := c.Validate()
	if dc.Fingerprint() == da.Fingerprint() {
		t.Fatal("other content kept the fingerprint")
	}
	e := a
	e.Attachments = []AttachmentSource{{FileName: "a.txt", Data: []byte("1")}}
	f := a
	f.Attachments = []AttachmentSource{{FileName: "a.txt", Data: []byte("2")}}
	de, _ := e.Validate()
	df, _ := f.Validate()
	if de.Fingerprint() == df.Fingerprint() {
		t.Fatal("other attachment bytes kept the fingerprint")
	}
}
