package node_executors

import (
	"errors"
	"testing"

	"vozko/domain/conversation"
	"vozko/domain/shared"
)

type recordingMediaRepo struct {
	conversation.ConversationMediaRepository
	created *conversation.ConversationMedia
	err     error
}

func (r *recordingMediaRepo) Create(m *conversation.ConversationMedia) error {
	if r.err != nil {
		return r.err
	}
	r.created = m
	return nil
}

func TestMediaBridgeGivesACaptionlessAttachmentContent(t *testing.T) {
	repo := &recordingMediaRepo{}

	mediaID, kind := bridgeConversationMedia(
		repo, "entry-1", shared.EntryTypeWhatsApp, "https://cdn.example/a.jpeg", "image")

	if mediaID == "" {
		t.Fatal("no media id: the record would still have no content without a caption")
	}
	if kind != conversation.MediaTypeImage {
		t.Errorf("kind = %q, want image", kind)
	}
	if repo.created.EntryID != "entry-1" || repo.created.EntryType != shared.EntryTypeWhatsApp {
		t.Errorf("media not scoped to the entry: %+v", repo.created)
	}
	if repo.created.URL != "https://cdn.example/a.jpeg" {
		t.Errorf("URL = %q", repo.created.URL)
	}

	msg := &conversation.Message{
		ID: "m1", EntryID: "entry-1", EntryType: shared.EntryTypeWhatsApp,
		From: "5511", To: "5522", Text: "", MediaID: &mediaID,
	}
	if err := msg.Validate(); err != nil {
		t.Errorf("a captionless media message is still invalid: %v", err)
	}
}

func TestMediaBridgeDegradesInsteadOfFailingTheSend(t *testing.T) {
	cases := map[string]struct {
		repo      conversation.ConversationMediaRepository
		entryID   string
		mediaURL  string
		mediaType string
	}{
		"repository unavailable": {nil, "entry-1", "https://cdn.example/a.jpeg", "image"},
		"repository errors":      {&recordingMediaRepo{err: errors.New("db down")}, "entry-1", "https://cdn.example/a.jpeg", "image"},
		"unknown media type":     {&recordingMediaRepo{}, "entry-1", "https://cdn.example/a.bin", "hologram"},
		"no url":                 {&recordingMediaRepo{}, "entry-1", "", "image"},
	}

	for name, tc := range cases {
		t.Run(name, func(t *testing.T) {
			id, kind := bridgeConversationMedia(tc.repo, tc.entryID, shared.EntryTypeWhatsApp, tc.mediaURL, tc.mediaType)
			if id != "" || kind != "" {
				t.Errorf("returned (%q, %q); a failed bridge must degrade to the caption-only record", id, kind)
			}
		})
	}
}
