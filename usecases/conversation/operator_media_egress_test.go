package conversation_usecase

import (
	"errors"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"vozko/domain/conversation"
	"vozko/domain/media"
	"vozko/domain/shared"
	"vozko/infra/netguard"
	"vozko/infra/remotefile"
)

func operatorMediaSender(url string, downloads media.RemoteFiles) (*MessageSenderService, *stubAudioMessageRepo) {
	msgRepo := &stubAudioMessageRepo{}
	svc := &MessageSenderService{
		whatsappRepo:          stubAudioEntryRepo{},
		leadRepo:              stubAudioLeadRepo{},
		messageWindowRepo:     stubAudioWindowRepo{},
		mediaRepo:             stubAudioMediaRepo{url: url},
		whatsappClientFactory: stubAudioClientFactory{client: stubAudioClient{out: &conversation.SendTextMessageOutput{MessageID: "wamid.1"}}},
		messageRepo:           msgRepo,
	}
	svc.SetMediaDownloads(downloads)
	return svc, msgRepo
}

func TestAnOperatorMediaSendRefusesInternalAddresses(t *testing.T) {
	internal := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "audio/ogg")
		_, _ = w.Write([]byte("internal"))
	}))
	defer internal.Close()

	for _, url := range []string{internal.URL + "/voice.ogg", "http://169.254.169.254/latest/meta-data/", "http://172.16.0.9/voice.ogg"} {
		t.Run(url, func(t *testing.T) {
			svc, msgRepo := operatorMediaSender(url, remotefile.NewDownloader(netguard.NewHTTPClient(2*time.Second)))
			_, err := svc.SendMediaMessage("entry-1", string(shared.EntryTypeWhatsApp), "media-1", "audio", "", conversation.SendOrigin{SenderUserID: "user-1"})
			if !errors.Is(err, shared.ErrEgressBlocked) {
				t.Fatalf("err = %v, want the egress refusal", err)
			}
			if len(msgRepo.created) != 0 {
				t.Fatal("a refused download must not record a message")
			}
		})
	}
}

func TestAnOperatorMediaSendWithoutTheGuardedDownloaderFetchesNothing(t *testing.T) {
	svc, _ := operatorMediaSender("https://cdn.example.com/voice.ogg", nil)
	_, err := svc.SendMediaMessage("entry-1", string(shared.EntryTypeWhatsApp), "media-1", "audio", "", conversation.SendOrigin{SenderUserID: "user-1"})
	if !errors.Is(err, media.ErrRemoteFilesUnwired) {
		t.Fatalf("err = %v, want the unwired refusal", err)
	}
}
