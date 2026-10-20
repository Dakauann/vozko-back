package template_infra

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"vozko/domain/conversation"
	"vozko/domain/shared"
	businessphone "vozko/domain/whatsapp/business_phone"
	"vozko/infra/netguard"
	"vozko/infra/remotefile"
)

type onePhone struct {
	businessphone.Repository
	phone *businessphone.WhatsAppBusinessPhoneNumber
}

func (r onePhone) FindByID(string) (*businessphone.WhatsAppBusinessPhoneNumber, error) {
	return r.phone, nil
}

func TestEveryClientTheFactoryBuildsDownloadsTemplateMediaThroughTheGuard(t *testing.T) {
	internal := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		_, _ = w.Write([]byte("internal"))
	}))
	defer internal.Close()

	for name, phone := range map[string]*businessphone.WhatsAppBusinessPhoneNumber{
		"meta":      {ID: "p1", MetaPhoneNumberID: "PN", WABAId: "WABA", AccessToken: "token", Provider: businessphone.ProviderMeta},
		"360dialog": {ID: "p2", MetaPhoneNumberID: "PN", WABAId: "WABA", Dialog360APIKey: "key", Provider: businessphone.ProviderDialog360},
	} {
		t.Run(name, func(t *testing.T) {
			factory := NewWhatsAppClientFactory(onePhone{phone: phone}, internal.Client(), internal.URL, "APP",
				remotefile.NewDownloader(netguard.NewHTTPClient(2*time.Second)))
			client, err := factory.ClientForPhone(phone.ID)
			if err != nil {
				t.Fatal(err)
			}
			_, err = client.UploadMediaForTemplate(context.Background(), conversation.UploadMediaForTemplateInput{URL: internal.URL + "/promo.png"})
			if !errors.Is(err, shared.ErrEgressBlocked) {
				t.Fatalf("err = %v, want the egress refusal", err)
			}
		})
	}
}
