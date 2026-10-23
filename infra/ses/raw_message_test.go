package ses

import (
	"bytes"
	"errors"
	"io"
	"mime"
	"mime/multipart"
	"mime/quotedprintable"
	"net/mail"
	"strings"
	"testing"
	"time"

	"vozko/domain/emailaddress"
	"vozko/domain/emailsending"
)

func sampleOutbound() emailsending.OutboundMessage {
	return emailsending.OutboundMessage{
		TenantName: "ws-1",
		From:       emailsending.Mailbox{Name: "Loja da Joana Ação", Address: "contato@joana.envios.test"},
		ReplyTo:    []emailaddress.Address{"reply+abc@joana.envios.test"},
		To:         "cliente@example.com",
		Subject:    "Promoção de outubro: até 50% off",
		HTML:       "<p>Olá, <b>cliente</b></p>",
		Text:       "Olá, cliente",
		Headers: []emailsending.Header{
			{Name: "List-Unsubscribe", Value: "<https://api.test/public/email/unsubscribe/tok>, <mailto:unsubscribe+tok@joana.envios.test>"},
			{Name: "List-Unsubscribe-Post", Value: "List-Unsubscribe=One-Click"},
			{Name: "In-Reply-To", Value: "<orig@example.com>"},
		},
		Kind: emailsending.KindMarketing,
	}
}

func decodeWords(t *testing.T, raw string) string {
	t.Helper()
	out, err := new(mime.WordDecoder).DecodeHeader(raw)
	if err != nil {
		t.Fatalf("decode %q: %v", raw, err)
	}
	return out
}

func TestBuildRawMessageMultipartAlternative(t *testing.T) {
	at := time.Date(2026, 10, 21, 12, 0, 0, 0, time.UTC)
	raw, err := buildRawMessage(sampleOutbound(), "11111111-2222-3333-4444-555555555555", at)
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
	from, err := mail.ParseAddress(msg.Header.Get("From"))
	if err != nil {
		t.Fatal(err)
	}
	if from.Name != "Loja da Joana Ação" || from.Address != "contato@joana.envios.test" {
		t.Fatalf("from = %+v", from)
	}
	if got := decodeWords(t, msg.Header.Get("Subject")); got != "Promoção de outubro: até 50% off" {
		t.Fatalf("subject = %q", got)
	}
	if msg.Header.Get("To") != "<cliente@example.com>" || msg.Header.Get("Reply-To") != "<reply+abc@joana.envios.test>" {
		t.Fatalf("to/reply-to = %q / %q", msg.Header.Get("To"), msg.Header.Get("Reply-To"))
	}
	if msg.Header.Get("Message-Id") != "<11111111-2222-3333-4444-555555555555@joana.envios.test>" {
		t.Fatalf("message-id = %q", msg.Header.Get("Message-Id"))
	}
	if msg.Header.Get("List-Unsubscribe-Post") != "List-Unsubscribe=One-Click" || !strings.HasPrefix(msg.Header.Get("List-Unsubscribe"), "<https://") {
		t.Fatalf("list-unsubscribe headers missing")
	}
	if msg.Header.Get("In-Reply-To") != "<orig@example.com>" {
		t.Fatalf("in-reply-to = %q", msg.Header.Get("In-Reply-To"))
	}
	if d, err := msg.Header.Date(); err != nil || !d.Equal(at) {
		t.Fatalf("date = %v %v", d, err)
	}
	mediaType, params, err := mime.ParseMediaType(msg.Header.Get("Content-Type"))
	if err != nil || mediaType != "multipart/alternative" {
		t.Fatalf("content type = %q %v", mediaType, err)
	}
	reader := multipart.NewReader(msg.Body, params["boundary"])
	var parts []string
	var bodies []string
	for {
		part, err := reader.NextRawPart()
		if errors.Is(err, io.EOF) {
			break
		}
		if err != nil {
			t.Fatal(err)
		}
		if part.Header.Get("Content-Transfer-Encoding") != "quoted-printable" {
			t.Fatalf("part encoding = %q", part.Header.Get("Content-Transfer-Encoding"))
		}
		body, _ := io.ReadAll(quotedprintable.NewReader(part))
		parts = append(parts, part.Header.Get("Content-Type"))
		bodies = append(bodies, string(body))
	}
	if strings.Join(parts, "|") != "text/plain; charset=UTF-8|text/html; charset=UTF-8" {
		t.Fatalf("parts = %v", parts)
	}
	if bodies[0] != "Olá, cliente" || bodies[1] != "<p>Olá, <b>cliente</b></p>" {
		t.Fatalf("bodies = %q", bodies)
	}
}

func TestBuildRawMessageSinglePart(t *testing.T) {
	m := sampleOutbound()
	m.HTML = ""
	m.Kind = emailsending.KindTransactional
	m.Headers = nil
	raw, err := buildRawMessage(m, "11111111-2222-3333-4444-555555555555", time.Now())
	if err != nil {
		t.Fatal(err)
	}
	msg, err := mail.ReadMessage(bytes.NewReader(raw))
	if err != nil {
		t.Fatal(err)
	}
	if msg.Header.Get("Content-Type") != "text/plain; charset=UTF-8" || msg.Header.Get("Content-Transfer-Encoding") != "quoted-printable" {
		t.Fatalf("headers = %v", msg.Header)
	}
}

func TestBuildRawMessageFoldsLongSubjects(t *testing.T) {
	m := sampleOutbound()
	m.Subject = strings.Repeat("Ação especial para você ", 20)
	raw, err := buildRawMessage(m, "11111111-2222-3333-4444-555555555555", time.Now())
	if err != nil {
		t.Fatal(err)
	}
	for _, line := range strings.Split(string(raw), "\r\n") {
		if len(line) > 998 {
			t.Fatalf("line longer than 998 octets: %d", len(line))
		}
	}
	msg, err := mail.ReadMessage(bytes.NewReader(raw))
	if err != nil {
		t.Fatal(err)
	}
	if got := decodeWords(t, msg.Header.Get("Subject")); got != m.Subject {
		t.Fatalf("subject round trip = %q", got)
	}
}

func TestBuildRawMessageRefusesHeaderInjection(t *testing.T) {
	cases := map[string]func(*emailsending.OutboundMessage){
		"subject newline":      func(m *emailsending.OutboundMessage) { m.Subject = "Oi\r\nBcc: x@y.com" },
		"display name newline": func(m *emailsending.OutboundMessage) { m.From.Name = "Loja\nBcc: x@y.com" },
		"header value newline": func(m *emailsending.OutboundMessage) {
			m.Headers = []emailsending.Header{{Name: "X-Test", Value: "a\r\nBcc: x"}}
		},
		"header name with colon": func(m *emailsending.OutboundMessage) {
			m.Headers = []emailsending.Header{{Name: "X-Test: y", Value: "a"}}
		},
		"header name with space": func(m *emailsending.OutboundMessage) { m.Headers = []emailsending.Header{{Name: "X Test", Value: "a"}} },
		"reserved header bcc": func(m *emailsending.OutboundMessage) {
			m.Headers = []emailsending.Header{{Name: "Bcc", Value: "x@y.com"}}
		},
		"reserved header from": func(m *emailsending.OutboundMessage) {
			m.Headers = []emailsending.Header{{Name: "from", Value: "x@y.com"}}
		},
		"nul in body":           func(m *emailsending.OutboundMessage) { m.Text = "a\x00b" },
		"to with newline":       func(m *emailsending.OutboundMessage) { m.To = "a@b.com\r\nBcc: c@d.com" },
		"reply-to with newline": func(m *emailsending.OutboundMessage) { m.ReplyTo = []emailaddress.Address{"a@b.com\nX: y"} },
		"empty body":            func(m *emailsending.OutboundMessage) { m.Text, m.HTML = "", "" },
	}
	for name, mutate := range cases {
		t.Run(name, func(t *testing.T) {
			m := sampleOutbound()
			mutate(&m)
			if _, err := buildRawMessage(m, "11111111-2222-3333-4444-555555555555", time.Now()); err == nil {
				t.Fatal("expected refusal")
			}
		})
	}
}

func TestSanitizeTags(t *testing.T) {
	got := sanitizeTags(map[string]string{
		"workspace":   "ws-1",
		"ses:tenant":  "x y",
		"run_kind":    "campaign",
		"":            "skip",
		"empty_value": "",
		"acentuação":  "valor/1",
	})
	want := map[string]string{
		"workspace":  "ws-1",
		"ses_tenant": "x_y",
		"run_kind":   "campaign",
		"acentua__o": "valor_1",
	}
	if len(got) != len(want) {
		t.Fatalf("got %v", got)
	}
	for _, tag := range got {
		if want[*tag.Name] != *tag.Value {
			t.Fatalf("tag %s=%s, want %s", *tag.Name, *tag.Value, want[*tag.Name])
		}
	}
	for i := 1; i < len(got); i++ {
		if *got[i-1].Name > *got[i].Name {
			t.Fatal("tags must be sorted for stable requests")
		}
	}
}
