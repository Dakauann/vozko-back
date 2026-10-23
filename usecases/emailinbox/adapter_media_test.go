package emailinbox

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"testing"

	"vozko/domain/balance"
	"vozko/domain/conversation"
	em "vozko/domain/emailinbox"
	"vozko/domain/emailsending"
	"vozko/domain/media"
)

type fakeRemoteFiles struct {
	files    map[string]media.RemoteFile
	err      error
	asked    []string
	maxAsked int64
}

func (f *fakeRemoteFiles) Download(_ context.Context, url string, maxBytes int64) (media.RemoteFile, error) {
	f.asked = append(f.asked, url)
	f.maxAsked = maxBytes
	if f.err != nil {
		return media.RemoteFile{}, f.err
	}
	file, ok := f.files[url]
	if !ok {
		return media.RemoteFile{}, media.ErrRemoteFileMissing
	}
	if int64(len(file.Data)) > maxBytes {
		return media.RemoteFile{}, fmt.Errorf("%w: over %d bytes", media.ErrMediaTooLarge, maxBytes)
	}
	return file, nil
}

const photoURL = "https://cdn.vozko.test/conversations/email/conv-1/foto-loja.png"

func (w *adapterWorld) withPhoto() []byte {
	data := []byte("\x89PNG\r\n\x1a\nfoto da loja")
	w.files.files[photoURL] = media.RemoteFile{Data: data, ContentType: "image/png"}
	return data
}

func TestSendMediaAttachesTheFileAndChargesItsEncodedSize(t *testing.T) {
	w := newAdapterWorld(t)
	data := w.withPhoto()
	outcome, err := w.adapter.SendMedia(context.Background(), w.entry(t), conversation.SendMediaRequest{
		Kind: "image", URL: photoURL, MIMEType: "image/png", FileName: "vitrine.png", Caption: "  Segue a <vitrine>.  ", HumanInitiated: true,
	})
	if err != nil {
		t.Fatal(err)
	}
	if strings.Join(w.order, ",") != "charge,send" {
		t.Fatalf("order = %v", w.order)
	}
	if len(w.billing.charges) != 1 || !strings.HasPrefix(w.billing.charges[0], wsA+"|email_reply:conv-1:") {
		t.Fatalf("charges = %v", w.billing.charges)
	}
	if want := emailsending.EncodedAttachmentBytes(int64(len(data))); len(w.billing.attachmentBytes) != 1 || w.billing.attachmentBytes[0] != want {
		t.Fatalf("charged attachment bytes = %v, want %d", w.billing.attachmentBytes, want)
	}
	if w.files.maxAsked != emailsending.MaxAttachmentBytes {
		t.Fatalf("download cap = %d, want the attachment budget", w.files.maxAsked)
	}
	req := w.gateway.requests[0]
	if req.Kind != emailsending.KindTransactional || req.RunKind != emailsending.RunReply || req.RunID != "conv-1" || req.InReplyTo != "m2@cliente.com" {
		t.Fatalf("request = %+v", req)
	}
	content := w.gateway.contents[0]
	if content.Subject != "Re: Oferta de outubro" || content.Text != "Segue a <vitrine>." || !strings.Contains(content.HTML, "&lt;vitrine&gt;") {
		t.Fatalf("content = %+v", content)
	}
	if len(content.Attachments) != 1 {
		t.Fatalf("attachments = %+v", content.Attachments)
	}
	att := content.Attachments[0]
	if att.FileName != "vitrine.png" || att.ContentType != "image/png" || string(att.Data) != string(data) {
		t.Fatalf("attachment = %q %q %d bytes", att.FileName, att.ContentType, len(att.Data))
	}
	if outcome.ProviderMessageID != "ses-conv-1" || outcome.Metadata[em.MetadataSubject] != "Re: Oferta de outubro" || outcome.Metadata[em.MetadataFileName] != "vitrine.png" {
		t.Fatalf("outcome = %+v", outcome)
	}
	if len(w.convs.outbound) != 1 {
		t.Fatal("outbound clock not advanced")
	}
}

func TestSendMediaNamesAndTypesTheFileFromItsOrigin(t *testing.T) {
	w := newAdapterWorld(t)
	w.withPhoto()
	if _, err := w.adapter.SendMedia(context.Background(), w.entry(t), conversation.SendMediaRequest{Kind: "image", URL: photoURL}); err != nil {
		t.Fatal(err)
	}
	content := w.gateway.contents[0]
	att := content.Attachments[0]
	if att.FileName != "foto-loja.png" || att.ContentType != "image/png" {
		t.Fatalf("attachment = %q %q", att.FileName, att.ContentType)
	}
	if content.Text != "foto-loja.png" {
		t.Fatalf("a reply without caption carries the file name as its text, got %q", content.Text)
	}
}

func TestSendMediaSendsTheBytesItWasHanded(t *testing.T) {
	w := newAdapterWorld(t)
	if _, err := w.adapter.SendMedia(context.Background(), w.entry(t), conversation.SendMediaRequest{
		Kind: "document", Bytes: []byte("%PDF-1.7 proposta"), FileName: "proposta", Caption: "Proposta",
	}); err != nil {
		t.Fatal(err)
	}
	if len(w.files.asked) != 0 {
		t.Fatalf("handed bytes were downloaded again: %v", w.files.asked)
	}
	att := w.gateway.contents[0].Attachments[0]
	if att.FileName != "proposta.pdf" || att.ContentType != "application/pdf" {
		t.Fatalf("attachment = %q %q", att.FileName, att.ContentType)
	}
}

func TestSendTextChargesNoAttachmentBytes(t *testing.T) {
	w := newAdapterWorld(t)
	if _, err := w.adapter.SendText(context.Background(), w.entry(t), conversation.SendTextRequest{Body: "oi"}); err != nil {
		t.Fatal(err)
	}
	if len(w.billing.attachmentBytes) != 1 || w.billing.attachmentBytes[0] != 0 || len(w.gateway.contents[0].Attachments) != 0 {
		t.Fatalf("attachment bytes = %v, attachments = %+v", w.billing.attachmentBytes, w.gateway.contents[0].Attachments)
	}
}

func TestSendMediaRefusals(t *testing.T) {
	cases := []struct {
		name        string
		req         conversation.SendMediaRequest
		files       map[string]media.RemoteFile
		downloadErr error
		short       bool
		sendErr     error
		wantErr     error
		wantCode    string
		wantSends   int
		wantRefunds int
	}{
		{
			name:     "a type the provider refuses is a coded refusal before any charge",
			req:      conversation.SendMediaRequest{Kind: "document", Bytes: []byte("MZ"), FileName: "instalador.exe"},
			wantErr:  emailsending.ErrAttachmentBlocked,
			wantCode: SendCodeAttachmentBlocked,
		},
		{
			name:     "a type derived from the file is refused too",
			req:      conversation.SendMediaRequest{Kind: "document", URL: "https://cdn.vozko.test/a/rotina.vbs"},
			files:    map[string]media.RemoteFile{"https://cdn.vozko.test/a/rotina.vbs": {Data: []byte("x"), ContentType: "text/plain"}},
			wantErr:  emailsending.ErrAttachmentBlocked,
			wantCode: SendCodeAttachmentBlocked,
		},
		{
			name:     "a file over the budget is a coded refusal",
			req:      conversation.SendMediaRequest{Kind: "video", URL: "https://cdn.vozko.test/a/longo.mp4"},
			files:    map[string]media.RemoteFile{"https://cdn.vozko.test/a/longo.mp4": {Data: make([]byte, emailsending.MaxAttachmentBytes+1), ContentType: "video/mp4"}},
			wantErr:  emailsending.ErrAttachmentTooLarge,
			wantCode: SendCodeAttachmentTooLarge,
		},
		{
			name:     "handed bytes over the budget are refused",
			req:      conversation.SendMediaRequest{Kind: "video", Bytes: make([]byte, emailsending.MaxAttachmentBytes+1), FileName: "longo.mp4"},
			wantErr:  emailsending.ErrAttachmentTooLarge,
			wantCode: SendCodeAttachmentTooLarge,
		},
		{
			name:     "a kind email does not carry",
			req:      conversation.SendMediaRequest{Kind: "sticker", Bytes: []byte("x"), FileName: "a.webp"},
			wantErr:  conversation.ErrCapabilityUnsupported,
			wantCode: "send_failed",
		},
		{
			name:        "a file that cannot be fetched is never charged",
			req:         conversation.SendMediaRequest{Kind: "image", URL: photoURL},
			downloadErr: media.ErrRemoteFileMissing,
			wantErr:     media.ErrRemoteFileMissing,
			wantCode:    "send_failed",
		},
		{
			name:     "no url and no bytes",
			req:      conversation.SendMediaRequest{Kind: "image"},
			wantErr:  conversation.ErrCapabilityUnsupported,
			wantCode: "send_failed",
		},
		{
			name:     "a caption over the text limit",
			req:      conversation.SendMediaRequest{Kind: "image", Bytes: []byte("x"), FileName: "a.png", Caption: strings.Repeat("a", em.MaxReplyTextBytes+1)},
			wantErr:  em.ErrReplyTooLong,
			wantCode: "send_failed",
		},
		{
			name:     "a short wallet sends nothing",
			req:      conversation.SendMediaRequest{Kind: "image", Bytes: []byte("x"), FileName: "a.png"},
			short:    true,
			wantErr:  balance.ErrInsufficientBalance,
			wantCode: SendCodeBalanceInsufficient,
		},
		{
			name:        "a provider failure refunds the charge",
			req:         conversation.SendMediaRequest{Kind: "image", Bytes: []byte("x"), FileName: "a.png"},
			sendErr:     emailsending.ErrAccountPaused,
			wantErr:     emailsending.ErrAccountPaused,
			wantCode:    "send_failed",
			wantSends:   1,
			wantRefunds: 1,
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			w := newAdapterWorld(t)
			for url, file := range tc.files {
				w.files.files[url] = file
			}
			w.files.err, w.billing.short, w.gateway.err = tc.downloadErr, tc.short, tc.sendErr
			_, err := w.adapter.SendMedia(context.Background(), w.entry(t), tc.req)
			if !errors.Is(err, tc.wantErr) {
				t.Fatalf("err = %v, want %v", err, tc.wantErr)
			}
			if got := conversation.SendFailureCode(err, "send_failed"); got != tc.wantCode {
				t.Errorf("code = %q, want %q", got, tc.wantCode)
			}
			if len(w.gateway.requests) != tc.wantSends || len(w.billing.refunds) != tc.wantRefunds {
				t.Errorf("sends=%d refunds=%d", len(w.gateway.requests), len(w.billing.refunds))
			}
			if tc.wantSends == 0 && len(w.billing.charges) != 0 {
				t.Errorf("a refusal before sending was charged: %v", w.billing.charges)
			}
			if tc.wantRefunds > 0 && w.billing.refunds[0] != w.billing.charges[0] {
				t.Errorf("refund %q does not match charge %q", w.billing.refunds[0], w.billing.charges[0])
			}
			if len(w.convs.outbound) != 0 {
				t.Error("a refused send must not advance the outbound clock")
			}
		})
	}
}

func TestSendMediaWithoutAnEntryIsRefused(t *testing.T) {
	w := newAdapterWorld(t)
	if _, err := w.adapter.SendMedia(context.Background(), nil, conversation.SendMediaRequest{Kind: "image", Bytes: []byte("x")}); !errors.Is(err, conversation.ErrNoAdapterForEntryType) {
		t.Fatalf("err = %v", err)
	}
}

func TestReplyRefusalCodesAreStableForTheComposer(t *testing.T) {
	for code, want := range map[string]string{
		SendCodeBalanceInsufficient: "email_balance_insufficient",
		SendCodeAttachmentBlocked:   "email_attachment_blocked",
		SendCodeAttachmentTooLarge:  "email_attachment_too_large",
	} {
		if code != want {
			t.Errorf("code %q drifted from %q, the front translates the latter", code, want)
		}
	}
}
