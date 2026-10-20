package tools_usecase

import (
	"context"
	"errors"
	"net/http"
	"testing"
	"time"

	"vozko/domain/media"
	"vozko/domain/shared"
	"vozko/infra/netguard"
	"vozko/infra/remotefile"
)

func loopbackDownloads() media.RemoteFiles {
	return remotefile.NewDownloader(&http.Client{Timeout: 5 * time.Second})
}

func nothingSent(c *recordingWhatsAppClient) bool {
	return c.sendImageInput == nil && c.sendVideoInput == nil && c.sendAudioInput == nil && c.sendDocumentInput == nil
}

func TestTheAgentMediaToolRefusesInternalAddressesWithoutFallingBackToTheLink(t *testing.T) {
	srv := servePNG(t)
	defer srv.Close()
	for _, item := range []media.Media{
		{ID: "img", URL: srv.URL + "/img.png", Type: media.MediaTypeProductImage},
		{ID: "vid", URL: "http://169.254.169.254/latest/clip.mp4", Type: media.MediaTypeProductVideo},
		{ID: "aud", URL: "http://10.0.0.5/voice.ogg", Type: media.MediaTypeAudio},
		{ID: "doc", URL: srv.URL + "/contract.pdf", Type: media.MediaTypeDocumentPdf},
	} {
		t.Run(item.ID, func(t *testing.T) {
			client := &recordingWhatsAppClient{}
			repo := &fakeMediaRepoForMediaTool{byID: map[string]*media.Media{item.ID: &item}}
			tool := NewSendWhatsappMediaToolUseCase(context.Background(), &stubFactory{c: client}, repo,
				remotefile.NewDownloader(netguard.NewHTTPClient(2*time.Second)))

			_, err := tool.ExecuteWithConfig(context.Background(), cfg(), map[string]interface{}{"to": "5511999999999", "media_id": item.ID})
			if !errors.Is(err, shared.ErrEgressBlocked) {
				t.Fatalf("err = %v, want the egress refusal", err)
			}
			if !nothingSent(client) {
				t.Fatal("a refused address was handed to WhatsApp as a link")
			}
		})
	}
}

func TestTheAgentMediaToolWithoutTheGuardedDownloaderSendsNothing(t *testing.T) {
	srv := servePNG(t)
	defer srv.Close()
	client := &recordingWhatsAppClient{}
	repo := &fakeMediaRepoForMediaTool{byID: map[string]*media.Media{"m1": {ID: "m1", URL: srv.URL + "/img.png", Type: media.MediaTypeProductImage}}}
	tool := NewSendWhatsappMediaToolUseCase(context.Background(), &stubFactory{c: client}, repo, nil)

	_, err := tool.ExecuteWithConfig(context.Background(), cfg(), map[string]interface{}{"to": "5511999999999", "media_id": "m1"})
	if !errors.Is(err, media.ErrRemoteFilesUnwired) {
		t.Fatalf("err = %v, want the unwired refusal", err)
	}
	if !nothingSent(client) {
		t.Fatal("nothing may go out without the guard")
	}
}
