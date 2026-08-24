package node_executors

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"testing"

	"vozko/domain/conversation"
)

// audioClient records which of the two audio paths the sender chose.
type audioClient struct {
	conversation.WhatsAppClient
	sentBytes []byte
	sentName  string
	linkURL   string
}

func (c *audioClient) SendAudioBytes(_ context.Context, _ string, data []byte, fileName, _ string) (*conversation.SendTextMessageOutput, error) {
	c.sentBytes = data
	c.sentName = fileName
	return &conversation.SendTextMessageOutput{MessageID: "wamid.voice"}, nil
}

func (c *audioClient) SendAudioMessage(_ context.Context, in conversation.SendAudioMessageInput) (*conversation.SendTextMessageOutput, error) {
	c.linkURL = in.AudioURL
	return &conversation.SendTextMessageOutput{MessageID: "wamid.link"}, nil
}

// A workflow's audio must be transcoded and uploaded, not handed over as a link.
//
// The Graph API accepts a link and then never delivers it: Cloud API voice notes
// must be OGG/Opus, and a stored .ogg is usually Vorbis. The workflow reported
// success while the customer received nothing, and the identical file sent by
// hand from the inbox arrived — because that path transcodes first.
func TestWorkflowAudioIsTranscodedAndUploaded(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		_, _ = w.Write([]byte("raw-vorbis-bytes"))
	}))
	defer srv.Close()

	original := convertAudioFn
	convertAudioFn = func(in []byte) ([]byte, error) { return []byte("opus:" + string(in)), nil }
	t.Cleanup(func() { convertAudioFn = original })

	client := &audioClient{}
	s := &whatsappSender{}

	out, err := s.sendAudioAsVoiceNote(context.Background(), client, "5511999999999", srv.URL+"/a.ogg")
	if err != nil {
		t.Fatalf("sendAudioAsVoiceNote: %v", err)
	}
	if out.MessageID != "wamid.voice" {
		t.Errorf("MessageID = %q, want the uploaded voice note", out.MessageID)
	}
	if string(client.sentBytes) != "opus:raw-vorbis-bytes" {
		t.Errorf("uploaded %q; the bytes were not transcoded", client.sentBytes)
	}
	if client.sentName != "voice.ogg" {
		t.Errorf("file name = %q, want voice.ogg", client.sentName)
	}
	if client.linkURL != "" {
		t.Errorf("fell back to link %q despite a working transcode", client.linkURL)
	}
}

// Transcoding shells out to ffmpeg. A host without it must degrade to the old
// link behaviour rather than dropping the message entirely.
func TestWorkflowAudioFallsBackToLinkWhenTranscodeFails(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		_, _ = w.Write([]byte("raw"))
	}))
	defer srv.Close()

	original := convertAudioFn
	convertAudioFn = func([]byte) ([]byte, error) { return nil, errors.New("ffmpeg not found") }
	t.Cleanup(func() { convertAudioFn = original })

	client := &audioClient{}
	s := &whatsappSender{}

	out, err := s.sendAudioAsVoiceNote(context.Background(), client, "5511999999999", srv.URL+"/a.ogg")
	if err != nil {
		t.Fatalf("sendAudioAsVoiceNote: %v", err)
	}
	if out.MessageID != "wamid.link" || client.linkURL == "" {
		t.Errorf("did not fall back to the link path (id=%q link=%q)", out.MessageID, client.linkURL)
	}
	if client.sentBytes != nil {
		t.Error("uploaded bytes even though the transcode failed")
	}
}

// An unreachable URL is the other way the transcode path cannot run.
func TestWorkflowAudioFallsBackToLinkWhenDownloadFails(t *testing.T) {
	client := &audioClient{}
	s := &whatsappSender{}

	out, err := s.sendAudioAsVoiceNote(context.Background(), client, "5511999999999", "http://127.0.0.1:1/nope.ogg")
	if err != nil {
		t.Fatalf("sendAudioAsVoiceNote: %v", err)
	}
	if out.MessageID != "wamid.link" {
		t.Errorf("MessageID = %q, want the link fallback", out.MessageID)
	}
}
