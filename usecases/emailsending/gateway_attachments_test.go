package emailsending

import (
	"context"
	"errors"
	"reflect"
	"testing"

	es "vozko/domain/emailsending"
)

func TestGatewayCarriesTheComposedAttachments(t *testing.T) {
	h, _ := readyHarness(t)
	req := marketingRequest()
	req.Kind = es.KindTransactional
	photo := es.Attachment{FileName: "foto.png", ContentType: "image/png", Data: []byte("png-bytes")}
	_, err := h.m.Gateway.Send(context.Background(), req, func(es.Envelope) (es.Content, error) {
		return es.Content{Subject: "Pedido", Text: "segue a foto", Attachments: []es.Attachment{photo}}, nil
	})
	if err != nil {
		t.Fatal(err)
	}
	if got := h.provider.sent[0].Attachments; !reflect.DeepEqual(got, []es.Attachment{photo}) {
		t.Fatalf("attachments = %+v", got)
	}
}

func TestGatewayRefusesABlockedAttachmentBeforeTheProvider(t *testing.T) {
	h, _ := readyHarness(t)
	req := marketingRequest()
	req.Kind = es.KindTransactional
	_, err := h.m.Gateway.Send(context.Background(), req, func(es.Envelope) (es.Content, error) {
		return es.Content{Subject: "Pedido", Text: "segue", Attachments: []es.Attachment{{FileName: "setup.exe", ContentType: "application/octet-stream", Data: []byte("MZ")}}}, nil
	})
	if !errors.Is(err, es.ErrAttachmentBlocked) {
		t.Fatalf("err = %v", err)
	}
	if len(h.provider.sent) != 0 {
		t.Fatal("a blocked attachment reached the provider")
	}
}
