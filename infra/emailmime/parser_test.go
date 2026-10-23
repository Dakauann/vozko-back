package emailmime

import (
	"bytes"
	"encoding/base64"
	"errors"
	"strings"
	"testing"
	"time"

	"vozko/domain/emailinbox"
)

func crlf(s string) []byte {
	return []byte(strings.ReplaceAll(strings.TrimPrefix(s, "\n"), "\n", "\r\n"))
}

const gmailAlternative = `
From: =?UTF-8?Q?Ana_Lu=C3=ADsa?= <Ana.Luisa@Gmail.com>
To: reply+abc@reply.loja.com.br, Outro <outro@x.com>
Cc: copia@x.com
Subject: =?UTF-8?B?UmU6IFBlZGlkbyBuwrogMTI=?=
Message-ID: <CAF123@mail.gmail.com>
In-Reply-To: <out-1@loja.com.br>
References: <root@loja.com.br>
 <out-1@loja.com.br>
Date: Tue, 21 Oct 2026 10:15:00 -0300
MIME-Version: 1.0
Content-Type: multipart/alternative; boundary="b1"

--b1
Content-Type: text/plain; charset="UTF-8"
Content-Transfer-Encoding: quoted-printable

Ol=C3=A1, pode enviar amanh=C3=A3?

Em seg., 20 de out. de 2026 =C3=A0s 10:00, Loja <loja@x.com> escreveu:
> Seu pedido saiu

--b1
Content-Type: text/html; charset="UTF-8"

<div>Ol&aacute;, <b>pode</b> enviar?</div>
--b1--
`

const latin1QuotedPrintable = `
From: Joao <joao@uol.com.br>
To: vendas@loja.com.br
Subject: =?ISO-8859-1?Q?Or=E7amento_r=E1pido?=
Message-ID: <q1@uol.com.br>
Content-Type: text/plain; charset=ISO-8859-1
Content-Transfer-Encoding: quoted-printable

Gostaria de um or=E7amento, por favor. A=E7=E3o r=E1pida!
`

const windows1252 = `
From: Maria <maria@x.com>
To: vendas@loja.com.br
Subject: Teste
Message-ID: <w1@x.com>
Content-Type: text/plain; charset=windows-1252

Pre` + "\x93" + `o` + "\x94" + ` final
`

const htmlOnly = `
From: Loja Cliente <c@x.com>
To: vendas@loja.com.br
Subject: Proposta
Message-ID: <h1@x.com>
Content-Type: text/html; charset=utf-8

<html><head><style>p{color:red}</style><script>alert(1)</script></head>
<body><p>Veja a <a href="https://x.com/proposta">proposta</a> anexa.</p><p>Linha&nbsp;2<br>Linha 3</p>
<img src="https://tracker.example/pixel.gif"></body></html>
`

const unknownCharset = `
From: k@x.com
To: vendas@loja.com.br
Subject: Hi
Message-ID: <k1@x.com>
Content-Type: text/plain; charset=koi8-r

plain ascii body
`

const autoReply = `
From: ferias@x.com
To: reply+abc@reply.loja.com.br
Subject: Ausente
Message-ID: <auto1@x.com>
Auto-Submitted: auto-replied
Content-Type: text/plain

Estou de ferias.
`

const noFrom = `
To: vendas@loja.com.br
Subject: Sem remetente
Content-Type: text/plain

oi
`

func mixedWithAttachments() []byte {
	pdf := base64.StdEncoding.EncodeToString([]byte("%PDF-1.4 fake"))
	png := base64.StdEncoding.EncodeToString([]byte("\x89PNG fake"))
	return crlf(`
From: Cliente <cliente@x.com>
To: reply+abc@reply.loja.com.br
Subject: Fwd: Nota
Message-ID: <att1@x.com>
Content-Type: multipart/mixed; boundary="outer"

--outer
Content-Type: multipart/related; boundary="rel"

--rel
Content-Type: multipart/alternative; boundary="alt"

--alt
Content-Type: text/plain; charset=utf-8

Segue a nota.
--alt
Content-Type: text/html; charset=utf-8

<p>Segue a nota.</p>
--alt--
--rel
Content-Type: image/png
Content-Transfer-Encoding: base64
Content-ID: <logo>
Content-Disposition: inline; filename="logo.png"

` + png + `
--rel--
--outer
Content-Type: application/pdf; name="=?UTF-8?Q?nota_fiscal_n=C2=BA1.pdf?="
Content-Transfer-Encoding: base64
Content-Disposition: attachment; filename*=UTF-8''nota%20fiscal%201.pdf

` + pdf + `
--outer--
`)
}

func TestParseMultipartAlternative(t *testing.T) {
	got, err := NewParser().Parse(crlf(gmailAlternative))
	if err != nil {
		t.Fatalf("Parse: %v", err)
	}
	if got.From.Address != "ana.luisa@gmail.com" || got.From.Name != "Ana Luísa" {
		t.Errorf("from = %+v", got.From)
	}
	if got.Subject != "Re: Pedido nº 12" {
		t.Errorf("subject = %q", got.Subject)
	}
	if got.MessageID != "CAF123@mail.gmail.com" || got.InReplyTo != "out-1@loja.com.br" {
		t.Errorf("ids = %q / %q", got.MessageID, got.InReplyTo)
	}
	if strings.Join(got.References, " ") != "root@loja.com.br out-1@loja.com.br" {
		t.Errorf("references = %v", got.References)
	}
	if len(got.To) != 3 || got.To[0] != "reply+abc@reply.loja.com.br" {
		t.Errorf("to = %v", got.To)
	}
	if got.Text != "Olá, pode enviar amanhã?" {
		t.Errorf("text = %q", got.Text)
	}
	if !strings.HasPrefix(got.QuotedText, "Em seg., 20 de out.") {
		t.Errorf("quoted = %q", got.QuotedText)
	}
	if want := time.Date(2026, 10, 21, 13, 15, 0, 0, time.UTC); !got.Date.Equal(want) {
		t.Errorf("date = %v", got.Date)
	}
	if got.Automated || len(got.Attachments) != 0 {
		t.Errorf("unexpected automated=%v attachments=%d", got.Automated, len(got.Attachments))
	}
}

func TestParseCharsets(t *testing.T) {
	cases := []struct {
		name, raw, subject, text string
	}{
		{"latin1 quoted printable", latin1QuotedPrintable, "Orçamento rápido", "Gostaria de um orçamento, por favor. Ação rápida!"},
		{"windows-1252", windows1252, "Teste", "Pre“o” final"},
		{"unknown charset keeps ascii", unknownCharset, "Hi", "plain ascii body"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got, err := NewParser().Parse(crlf(tc.raw))
			if err != nil {
				t.Fatalf("Parse: %v", err)
			}
			if got.Subject != tc.subject || got.Text != tc.text {
				t.Errorf("got subject %q text %q", got.Subject, got.Text)
			}
		})
	}
}

func TestParseHTMLOnlyNeverKeepsMarkup(t *testing.T) {
	got, err := NewParser().Parse(crlf(htmlOnly))
	if err != nil {
		t.Fatalf("Parse: %v", err)
	}
	want := "Veja a proposta (https://x.com/proposta) anexa.\n\nLinha 2\nLinha 3"
	if got.Text != want {
		t.Errorf("text = %q, want %q", got.Text, want)
	}
	for _, leaked := range []string{"<", "alert", "color:red", "tracker"} {
		if strings.Contains(got.Text, leaked) {
			t.Errorf("text leaked %q: %q", leaked, got.Text)
		}
	}
}

func TestParseAttachments(t *testing.T) {
	got, err := NewParser().Parse(mixedWithAttachments())
	if err != nil {
		t.Fatalf("Parse: %v", err)
	}
	if got.Text != "Segue a nota." {
		t.Errorf("text = %q", got.Text)
	}
	if len(got.Attachments) != 2 {
		t.Fatalf("attachments = %d", len(got.Attachments))
	}
	logo, pdf := got.Attachments[0], got.Attachments[1]
	if logo.FileName != "logo.png" || logo.ContentType != "image/png" || !logo.Inline || !bytes.HasPrefix(logo.Data, []byte("\x89PNG")) {
		t.Errorf("logo = %+v", logo)
	}
	if pdf.FileName != "nota fiscal 1.pdf" || pdf.ContentType != "application/pdf" || pdf.Inline || string(pdf.Data) != "%PDF-1.4 fake" || pdf.Size != int64(len(pdf.Data)) {
		t.Errorf("pdf = %+v", pdf)
	}
}

func TestParseOversizedAttachmentKeepsOnlyItsName(t *testing.T) {
	parser := Parser{maxAttachmentBytes: 4}
	got, err := parser.Parse(mixedWithAttachments())
	if err != nil {
		t.Fatalf("Parse: %v", err)
	}
	for _, a := range got.Attachments {
		if a.Data != nil || a.Size <= 4 || a.FileName == "" {
			t.Errorf("oversized attachment kept its bytes: %+v", a)
		}
	}
}

func TestParseAutomated(t *testing.T) {
	got, err := NewParser().Parse(crlf(autoReply))
	if err != nil {
		t.Fatalf("Parse: %v", err)
	}
	if !got.Automated {
		t.Error("Auto-Submitted: auto-replied must mark the message automated")
	}
}

func TestParseRefusesWhatCannotBeRead(t *testing.T) {
	cases := map[string][]byte{
		"no from": crlf(noFrom),
		"garbage": []byte("not an email at all"),
		"empty":   nil,
	}
	for name, raw := range cases {
		t.Run(name, func(t *testing.T) {
			_, err := NewParser().Parse(raw)
			if !errors.Is(err, emailinbox.ErrUnparseableMessage) && !errors.Is(err, emailinbox.ErrNoSender) {
				t.Fatalf("err = %v", err)
			}
		})
	}
}
