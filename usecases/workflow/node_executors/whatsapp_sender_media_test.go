package node_executors

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"testing"

	"vozko/domain/conversation"
	lead_domain "vozko/domain/lead"
	"vozko/domain/shared"
	wce "vozko/domain/whatsapp_campaign_entry"
	"vozko/domain/workflow"
)

type stubMediaEntryRepo struct{}

func (stubMediaEntryRepo) FindByID(id string) (*wce.WhatsAppCampaignEntry, error) {
	return &wce.WhatsAppCampaignEntry{ID: id, LeadID: "lead-1", ReceivedBusinessPhoneID: "phone-1"}, nil
}

func (stubMediaEntryRepo) GetCampaignForEntry(string) (*wce.EntryCampaignInfo, error) {
	return &wce.EntryCampaignInfo{CampaignID: "camp-1", WorkspaceID: "ws-1", BusinessPhoneID: "phone-1"}, nil
}

type stubMediaLeadRepo struct{}

func (stubMediaLeadRepo) FindByID(workspaceID, id string) (*lead_domain.Lead, error) {
	return &lead_domain.Lead{ID: id, WorkspaceID: workspaceID, Number: "5511999999999"}, nil
}

// mediaSendClient records which send path the sender actually took, so the
// transcoded voice note and the link fallback can be told apart.
type mediaSendClient struct {
	conversation.WhatsAppClient

	voiceBytes    []byte
	voiceFilename string
	linkAudioURL  string
	imageID       string
}

func (c *mediaSendClient) SendAudioBytes(_ context.Context, _ string, data []byte, filename, _ string) (*conversation.SendTextMessageOutput, error) {
	c.voiceBytes = data
	c.voiceFilename = filename
	return &conversation.SendTextMessageOutput{MessageID: "wamid.voice"}, nil
}

func (c *mediaSendClient) SendAudioMessage(_ context.Context, input conversation.SendAudioMessageInput) (*conversation.SendTextMessageOutput, error) {
	c.linkAudioURL = input.AudioURL
	return &conversation.SendTextMessageOutput{MessageID: "wamid.link"}, nil
}

func (c *mediaSendClient) SendImageMessage(_ context.Context, input conversation.SendImageMessageInput) (*conversation.SendTextMessageOutput, error) {
	c.imageID = input.ImageID
	return &conversation.SendTextMessageOutput{MessageID: "wamid.image"}, nil
}

func (c *mediaSendClient) UploadMedia(context.Context, []byte, string, string) (string, error) {
	return "wa-media-1", nil
}

type stubMediaClientFactory struct{ client conversation.WhatsAppClient }

func (f stubMediaClientFactory) ClientForPhone(string) (conversation.WhatsAppClient, error) {
	return f.client, nil
}

func (f stubMediaClientFactory) ClientForWABA(string) (conversation.WhatsAppClient, error) {
	return f.client, nil
}

func (stubMediaClientFactory) WABAIdForPhone(string) (string, error) { return "waba-1", nil }

type recordingConversationMedia struct {
	conversation.ConversationMediaRepository
	created []*conversation.ConversationMedia
	err     error
}

func (r *recordingConversationMedia) Create(m *conversation.ConversationMedia) error {
	if r.err != nil {
		return r.err
	}
	r.created = append(r.created, m)
	return nil
}

func waMediaRun() *workflow.WorkflowRun {
	return &workflow.WorkflowRun{
		ID:          "run-1",
		EntryID:     "11111111-1111-1111-1111-111111111111",
		EntryType:   string(shared.EntryTypeWhatsApp),
		WorkspaceID: "ws-1",
	}
}

func mediaServer(t *testing.T, contentType string, body []byte) *httptest.Server {
	t.Helper()
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", contentType)
		_, _ = w.Write(body)
	}))
	t.Cleanup(server.Close)
	return server
}

func newMediaSender(client conversation.WhatsAppClient, history conversation.MessageHistoryManager, mediaRepo conversation.ConversationMediaRepository) *whatsappSender {
	return &whatsappSender{deps: SenderDeps{
		ClientFactory:         stubMediaClientFactory{client: client},
		LeadRepo:              stubMediaLeadRepo{},
		WhatsAppEntryRepo:     stubMediaEntryRepo{},
		HistoryManager:        history,
		ConversationMediaRepo: mediaRepo,
	}}
}

// stubConvertAudio swaps the ffmpeg call out, so both branches run on a host
// that does not have ffmpeg installed.
func stubConvertAudio(t *testing.T, fn func([]byte) ([]byte, error)) {
	t.Helper()
	original := convertAudioFn
	convertAudioFn = fn
	t.Cleanup(func() { convertAudioFn = original })
}

// Regression cover: the workflow audio node used to hand WhatsApp the stored
// CDN link. Graph accepts it and then never delivers, because a Cloud API voice
// note must be OGG/Opus and a stored .ogg is usually Vorbis. The run reported a
// successful send for a message the customer never received.
func TestSendMediaTranscodesAudioIntoAVoiceNote(t *testing.T) {
	server := mediaServer(t, "audio/ogg", []byte("raw-vorbis"))
	stubConvertAudio(t, func(in []byte) ([]byte, error) {
		return append([]byte("opus:"), in...), nil
	})

	client := &mediaSendClient{}
	history := &recordingHistory{}
	sender := newMediaSender(client, history, nil)

	out, _, err := sender.SendMedia(context.Background(), waMediaRun(), server.URL+"/note.ogg", "")
	if err != nil {
		t.Fatalf("SendMedia returned error: %v", err)
	}
	if out == nil || out.MessageID != "wamid.voice" {
		t.Fatalf("expected the voice-note send, got %+v", out)
	}
	if got := string(client.voiceBytes); got != "opus:raw-vorbis" {
		t.Fatalf("expected the transcoded bytes, got %q", got)
	}
	if client.voiceFilename != "voice.ogg" {
		t.Fatalf("expected voice.ogg, got %q", client.voiceFilename)
	}
	if client.linkAudioURL != "" {
		t.Fatalf("link path was taken as well: %q", client.linkAudioURL)
	}
}

// A host with no ffmpeg must degrade to the old link send rather than fail the
// node outright: an undelivered message is bad, a broken workflow is worse.
func TestSendMediaFallsBackToTheLinkWhenTranscodeFails(t *testing.T) {
	server := mediaServer(t, "audio/ogg", []byte("raw-vorbis"))
	stubConvertAudio(t, func([]byte) ([]byte, error) {
		return nil, errors.New("exec: ffmpeg: executable file not found in $PATH")
	})

	client := &mediaSendClient{}
	sender := newMediaSender(client, &recordingHistory{}, nil)

	mediaURL := server.URL + "/note.ogg"
	out, _, err := sender.SendMedia(context.Background(), waMediaRun(), mediaURL, "")
	if err != nil {
		t.Fatalf("SendMedia returned error: %v", err)
	}
	if out == nil || out.MessageID != "wamid.link" {
		t.Fatalf("expected the link fallback, got %+v", out)
	}
	if client.linkAudioURL != mediaURL {
		t.Fatalf("expected the link send to carry %q, got %q", mediaURL, client.linkAudioURL)
	}
	if client.voiceBytes != nil {
		t.Fatalf("voice-note path ran despite the transcode failure")
	}
}

// Regression cover: a captionless attachment produced a history record with no
// text and no MediaID, which Message.Validate rejects for having no content —
// so a file the customer had already received was recorded as a failed send.
func TestSendMediaBridgesACaptionlessAttachmentIntoConversationMedia(t *testing.T) {
	server := mediaServer(t, "image/png", []byte("png-bytes"))
	mediaRepo := &recordingConversationMedia{}
	history := &recordingHistory{}
	run := waMediaRun()

	mediaURL := server.URL + "/photo.png"
	if _, _, err := newMediaSender(&mediaSendClient{}, history, mediaRepo).
		SendMedia(context.Background(), run, mediaURL, ""); err != nil {
		t.Fatalf("SendMedia returned error: %v", err)
	}

	if len(mediaRepo.created) != 1 {
		t.Fatalf("expected one conversation_media row, got %d", len(mediaRepo.created))
	}
	created := mediaRepo.created[0]
	if created.EntryID != run.EntryID || created.EntryType != shared.EntryTypeWhatsApp {
		t.Fatalf("row is attached to the wrong entry: %+v", created)
	}
	if created.Type != conversation.MediaTypeImage || created.URL != mediaURL {
		t.Fatalf("unexpected row contents: %+v", created)
	}
	if created.OriginalFilename != "photo.png" {
		t.Fatalf("expected photo.png, got %q", created.OriginalFilename)
	}

	if len(history.records) != 1 {
		t.Fatalf("expected one history record, got %d", len(history.records))
	}
	record := history.records[0]
	if record.MediaID != created.ID {
		t.Fatalf("history record points at %q, row is %q", record.MediaID, created.ID)
	}
	if record.MediaType != conversation.MediaTypeImage || record.MediaURL != mediaURL {
		t.Fatalf("unexpected media fields on the record: %+v", record)
	}
	// The MediaID is the whole point: with an empty caption it is the only
	// content Message.Validate will accept.
	if record.Text != "" || record.MediaID == "" {
		t.Fatalf("captionless record has no content: %+v", record)
	}
}

// The message is already delivered by the time the bridge runs, so a repository
// failure must cost the thumbnail and nothing else.
func TestSendMediaSurvivesAConversationMediaFailure(t *testing.T) {
	server := mediaServer(t, "image/png", []byte("png-bytes"))
	mediaRepo := &recordingConversationMedia{err: errors.New("insert failed")}
	history := &recordingHistory{}

	out, _, err := newMediaSender(&mediaSendClient{}, history, mediaRepo).
		SendMedia(context.Background(), waMediaRun(), server.URL+"/photo.png", "legenda")
	if err != nil {
		t.Fatalf("a bridging failure broke the send: %v", err)
	}
	if out == nil || out.MessageID != "wamid.image" {
		t.Fatalf("expected the image send to stand, got %+v", out)
	}
	if len(history.records) != 1 || history.records[0].MediaID != "" {
		t.Fatalf("expected one record with no media id, got %+v", history.records)
	}
}
