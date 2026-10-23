package ses

import (
	"bytes"
	"errors"
	"mime"
	"net/mail"
	"strings"
	"testing"
	"time"

	"vozko/domain/emailsending"
	"vozko/infra/emailmime"
)

var attachmentsAt = time.Date(2026, 10, 22, 12, 0, 0, 0, time.UTC)

func replyWithAttachments(atts ...emailsending.Attachment) emailsending.OutboundMessage {
	m := sampleOutbound()
	m.Kind = emailsending.KindTransactional
	m.Headers = nil
	m.Attachments = atts
	return m
}

func TestBuildRawMessageWithAttachmentsIsMultipartMixed(t *testing.T) {
	photo := emailsending.Attachment{FileName: "foto ação.png", ContentType: "image/png", Data: bytes.Repeat([]byte{0x89, 'P', 'N', 'G'}, 100)}
	pdf := emailsending.Attachment{FileName: "contrato.pdf", ContentType: "application/pdf", Data: []byte("%PDF-1.7 conteudo")}
	raw, err := buildRawMessage(replyWithAttachments(photo, pdf), "11111111-2222-3333-4444-555555555555", attachmentsAt)
	if err != nil {
		t.Fatal(err)
	}
	if bytes.Contains(bytes.ReplaceAll(raw, []byte("\r\n"), nil), []byte("\n")) {
		t.Fatal("every line must end with CRLF")
	}
	msg, err := mail.ReadMessage(bytes.NewReader(raw))
	if err != nil {
		t.Fatal(err)
	}
	mediaType, _, err := mime.ParseMediaType(msg.Header.Get("Content-Type"))
	if err != nil || mediaType != "multipart/mixed" {
		t.Fatalf("content type = %q (%v)", msg.Header.Get("Content-Type"), err)
	}
	for _, line := range strings.Split(string(raw), "\r\n") {
		if len(line) > 998 {
			t.Fatalf("a line of %d bytes breaks RFC 5322", len(line))
		}
	}

	parsed, err := emailmime.NewParser().Parse(raw)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(parsed.Text, "Olá, cliente") {
		t.Fatalf("text = %q", parsed.Text)
	}
	if len(parsed.Attachments) != 2 {
		t.Fatalf("attachments = %+v", parsed.Attachments)
	}
	for i, want := range []emailsending.Attachment{photo, pdf} {
		got := parsed.Attachments[i]
		if got.FileName != want.FileName || !strings.HasPrefix(got.ContentType, want.ContentType) || !bytes.Equal(got.Data, want.Data) {
			t.Errorf("attachment %d = %q %q %d bytes, want %q %q %d bytes", i, got.FileName, got.ContentType, len(got.Data), want.FileName, want.ContentType, len(want.Data))
		}
	}
}

func TestBuildRawMessageWithAttachmentsAndOnlyText(t *testing.T) {
	m := replyWithAttachments(emailsending.Attachment{FileName: "nota.txt", ContentType: "text/plain", Data: []byte("anotado")})
	m.HTML = ""
	raw, err := buildRawMessage(m, "11111111-2222-3333-4444-555555555555", attachmentsAt)
	if err != nil {
		t.Fatal(err)
	}
	parsed, err := emailmime.NewParser().Parse(raw)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(parsed.Text, "Olá, cliente") || len(parsed.Attachments) != 1 || string(parsed.Attachments[0].Data) != "anotado" {
		t.Fatalf("parsed = %q %+v", parsed.Text, parsed.Attachments)
	}
}

func TestBuildRawMessageRefusesWhatTheProviderWouldRefuse(t *testing.T) {
	cases := []struct {
		name string
		att  emailsending.Attachment
		want error
	}{
		{"a blocked type", emailsending.Attachment{FileName: "setup.exe", ContentType: "application/octet-stream", Data: []byte("MZ")}, emailsending.ErrAttachmentBlocked},
		{"a header break in the name", emailsending.Attachment{FileName: "a\r\nBcc: x@y.z", ContentType: "image/png", Data: []byte("x")}, emailsending.ErrAttachmentInvalid},
		{"over the budget", emailsending.Attachment{FileName: "big.pdf", ContentType: "application/pdf", Data: make([]byte, emailsending.MaxAttachmentBytes+1)}, emailsending.ErrAttachmentTooLarge},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			_, err := buildRawMessage(replyWithAttachments(tc.att), "11111111-2222-3333-4444-555555555555", attachmentsAt)
			if !errors.Is(err, tc.want) {
				t.Fatalf("err = %v, want %v", err, tc.want)
			}
		})
	}
}

func TestBuildRawMessageRefusesAMessageOverTheDeliverableSize(t *testing.T) {
	m := replyWithAttachments(emailsending.Attachment{FileName: "full.bin", ContentType: "application/octet-stream", Data: make([]byte, emailsending.MaxAttachmentBytes)})
	if _, err := buildRawMessage(m, "11111111-2222-3333-4444-555555555555", attachmentsAt); err != nil {
		t.Fatalf("a full budget must still be deliverable: %v", err)
	}
	m.Text = strings.Repeat("x", int(emailsending.MaxDeliverableMessageBytes))
	if _, err := buildRawMessage(m, "11111111-2222-3333-4444-555555555555", attachmentsAt); !errors.Is(err, emailsending.ErrMessageTooLarge) {
		t.Fatalf("err = %v, a message over the deliverable size must be refused", err)
	}
}
