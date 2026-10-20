package node_executors

import (
	"context"
	"errors"
	"net/http"
	"testing"
	"time"

	"vozko/domain/conversation"
	"vozko/domain/media"
	"vozko/domain/shared"
	"vozko/infra/netguard"
	"vozko/infra/remotefile"
)

func unguardedTestFetcher() MediaFetcher {
	return MediaFetcherOf(remotefile.NewDownloader(&http.Client{Timeout: 5 * time.Second}))
}

func guardedTestFetcher() MediaFetcher {
	return MediaFetcherOf(remotefile.NewDownloader(netguard.NewHTTPClient(2 * time.Second)))
}

type countingMediaClient struct {
	mediaSendClient
	sends int
}

func (c *countingMediaClient) SendImageMessage(ctx context.Context, input conversation.SendImageMessageInput) (*conversation.SendTextMessageOutput, error) {
	c.sends++
	return c.mediaSendClient.SendImageMessage(ctx, input)
}

func (c *countingMediaClient) SendAudioMessage(ctx context.Context, input conversation.SendAudioMessageInput) (*conversation.SendTextMessageOutput, error) {
	c.sends++
	return c.mediaSendClient.SendAudioMessage(ctx, input)
}

func (c *countingMediaClient) SendAudioBytes(ctx context.Context, to string, data []byte, filename, caption string) (*conversation.SendTextMessageOutput, error) {
	c.sends++
	return c.mediaSendClient.SendAudioBytes(ctx, to, data, filename, caption)
}

func TestAWorkflowMediaSendRefusesInternalAddressesWithoutFallingBackToTheLink(t *testing.T) {
	server := mediaServer(t, "image/png", []byte("internal"))
	for _, url := range []string{
		server.URL + "/photo.png",
		"http://169.254.169.254/latest/meta-data/photo.png",
		"http://10.1.2.3/photo.png",
		server.URL + "/note.ogg",
	} {
		t.Run(url, func(t *testing.T) {
			client := &countingMediaClient{}
			history := &recordingHistory{}
			sender := newMediaSender(client, history, nil)
			sender.deps.MediaFetcher = guardedTestFetcher()

			_, _, err := sender.SendMedia(context.Background(), waMediaRun(), url, "")
			if !errors.Is(err, shared.ErrEgressBlocked) {
				t.Fatalf("err = %v, want the egress refusal", err)
			}
			if client.sends != 0 || len(history.records) != 0 {
				t.Fatalf("sends = %d, records = %d; a refused address must not be handed to WhatsApp as a link", client.sends, len(history.records))
			}
		})
	}
}

func TestAWorkflowMediaSendWithoutTheGuardedDownloaderFetchesNothing(t *testing.T) {
	server := mediaServer(t, "image/png", []byte("png"))
	client := &countingMediaClient{}
	sender := newMediaSender(client, &recordingHistory{}, nil)
	sender.deps.MediaFetcher = nil

	_, _, err := sender.SendMedia(context.Background(), waMediaRun(), server.URL+"/photo.png", "")
	if !errors.Is(err, media.ErrRemoteFilesUnwired) {
		t.Fatalf("err = %v, want the unwired refusal", err)
	}
	if client.sends != 0 {
		t.Fatalf("sends = %d, nothing may go out without the guard", client.sends)
	}
}
