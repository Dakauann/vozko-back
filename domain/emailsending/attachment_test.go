package emailsending

import (
	"bytes"
	"encoding/base64"
	"errors"
	"strings"
	"testing"
)

func TestTheAttachmentBudgetStaysDeliverableAfterEncoding(t *testing.T) {
	reserve := int64(1 << 20)
	if got := EncodedAttachmentBytes(MaxAttachmentBytes) + reserve; got > MaxDeliverableMessageBytes {
		t.Fatalf("a full attachment budget encodes to %d bytes with the body reserve, over the %d deliverable bytes", got, MaxDeliverableMessageBytes)
	}
	if MaxDeliverableMessageBytes > MaxRawMessageBytes {
		t.Fatalf("the deliverable size %d is over the provider limit %d", MaxDeliverableMessageBytes, int64(MaxRawMessageBytes))
	}
}

func TestEncodedAttachmentBytesMatchesWrappedBase64(t *testing.T) {
	for _, size := range []int{1, 2, 3, 56, 57, 58, 1000, 256 << 10} {
		data := bytes.Repeat([]byte{0xAB}, size)
		encoded := base64.StdEncoding.EncodeToString(data)
		lines := (len(encoded) + 75) / 76
		want := int64(len(encoded) + 2*lines)
		if got := EncodedAttachmentBytes(int64(size)); got != want {
			t.Errorf("EncodedAttachmentBytes(%d) = %d, want %d", size, got, want)
		}
	}
	if EncodedAttachmentBytes(0) != 0 || EncodedAttachmentBytes(-4) != 0 {
		t.Fatal("an empty attachment encodes to nothing")
	}
}

func TestAttachmentChunksRoundUpBecauseEveryByteSentIsBilled(t *testing.T) {
	cases := []struct {
		bytes int64
		want  int64
	}{
		{0, 0},
		{-1, 0},
		{1, 1},
		{OutboundChunkBytes, 1},
		{OutboundChunkBytes + 1, 2},
		{3 * OutboundChunkBytes, 3},
	}
	for _, tc := range cases {
		if got := AttachmentChunks(tc.bytes); got != tc.want {
			t.Errorf("AttachmentChunks(%d) = %d, want %d", tc.bytes, got, tc.want)
		}
	}
	if OutboundChunkBytes != InboundChunkBytes {
		t.Fatal("sent and received attachment data are billed in the same chunk unit")
	}
}

func TestBlockedAttachmentFollowsTheProviderList(t *testing.T) {
	for _, name := range []string{"setup.exe", "RUN.BAT", "script.js", "macro.vbs", "x.ps1", "a.msi", "b.lnk", "c.scr", "d.jse", "e.wsf", "f.cmd", "g.com", "h.reg", "i.hta", "j.msh2xml", "trick.exe.", "space.exe "} {
		if !BlockedAttachment(name) {
			t.Errorf("%q must be refused", name)
		}
	}
	for _, name := range []string{"foto.jpg", "contrato.pdf", "planilha.xlsx", "audio.ogg", "video.mp4", "exe", "notes.txt", "arquivo"} {
		if BlockedAttachment(name) {
			t.Errorf("%q must be accepted", name)
		}
	}
}

func TestCheckAttachments(t *testing.T) {
	ok := Attachment{FileName: "foto.png", ContentType: "image/png", Data: []byte("png")}
	cases := []struct {
		name string
		atts []Attachment
		want error
	}{
		{"none", nil, nil},
		{"one image", []Attachment{ok}, nil},
		{"a blocked type", []Attachment{{FileName: "setup.exe", ContentType: "application/octet-stream", Data: []byte("MZ")}}, ErrAttachmentBlocked},
		{"no bytes", []Attachment{{FileName: "foto.png", ContentType: "image/png"}}, ErrAttachmentInvalid},
		{"no name", []Attachment{{FileName: " ", ContentType: "image/png", Data: []byte("x")}}, ErrAttachmentInvalid},
		{"a header break in the name", []Attachment{{FileName: "a\r\nBcc: x@y.z.png", ContentType: "image/png", Data: []byte("x")}}, ErrAttachmentInvalid},
		{"a malformed type", []Attachment{{FileName: "a.png", ContentType: "png", Data: []byte("x")}}, ErrAttachmentInvalid},
		{"over the budget", []Attachment{{FileName: "big.pdf", ContentType: "application/pdf", Data: make([]byte, MaxAttachmentBytes+1)}}, ErrAttachmentTooLarge},
		{"over the budget together", []Attachment{
			{FileName: "a.pdf", ContentType: "application/pdf", Data: make([]byte, MaxAttachmentBytes/2+1)},
			{FileName: "b.pdf", ContentType: "application/pdf", Data: make([]byte, MaxAttachmentBytes/2)},
		}, ErrAttachmentTooLarge},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			err := CheckAttachments(tc.atts)
			if tc.want == nil && err != nil || tc.want != nil && !errors.Is(err, tc.want) {
				t.Fatalf("CheckAttachments = %v, want %v", err, tc.want)
			}
		})
	}
}

func TestAnOutboundMessageWithABlockedAttachmentIsInvalid(t *testing.T) {
	m := OutboundMessage{
		TenantName: "t", From: Mailbox{Address: "a@loja.example"}, To: "b@cliente.example", Subject: "Oi", Text: "oi", Kind: KindTransactional,
		Attachments: []Attachment{{FileName: "virus.vbs", ContentType: "text/plain", Data: []byte("x")}},
	}
	if err := m.Validate(); !errors.Is(err, ErrAttachmentBlocked) {
		t.Fatalf("Validate = %v", err)
	}
	m.Attachments = []Attachment{{FileName: "foto.png", ContentType: "image/png", Data: []byte("x")}}
	if err := m.Validate(); err != nil {
		t.Fatalf("Validate = %v", err)
	}
}

func TestAttachmentNameKeepsAnExtension(t *testing.T) {
	cases := []struct{ name, contentType, want string }{
		{"foto.png", "image/png", "foto.png"},
		{"", "image/png", "anexo.png"},
		{"contrato", "application/pdf", "contrato.pdf"},
		{"  ", "application/x-unknown-thing", "anexo"},
		{"pasta/sub/foto.jpg", "image/jpeg", "foto.jpg"},
	}
	for _, tc := range cases {
		if got := AttachmentName(tc.name, tc.contentType); got != tc.want {
			t.Errorf("AttachmentName(%q, %q) = %q, want %q", tc.name, tc.contentType, got, tc.want)
		}
	}
	if strings.ContainsAny(AttachmentName("a\r\nb.png", "image/png"), "\r\n") {
		t.Fatal("line breaks never survive into a file name")
	}
}
