package conversation_usecase

import (
	"context"
	"errors"
	"testing"

	"vozko/domain/conversation"
	"vozko/domain/media"
	"vozko/domain/shared"
)

// A campaign's attachment lives in the workspace media library, not in
// conversation media.
//
// The send resolved it from the per-conversation store instead, where a library
// id can never exist, so the first recipient of every media campaign failed with
// "campaign send: media not found: conversation: media not found" and no
// attachment was ever delivered on this channel.

type campaignLibraryStub struct {
	media.MediaRepository
	row *media.Media
	err error
	ids []string
}

func (s *campaignLibraryStub) GetMediaByID(id string) (*media.Media, error) {
	s.ids = append(s.ids, id)
	return s.row, s.err
}

// campaignConvMediaStub is the WRONG store for this lookup. GetByID fails the
// test outright: consulting it at all is the bug.
type campaignConvMediaStub struct {
	conversation.ConversationMediaRepository
	t       *testing.T
	created []*conversation.ConversationMedia
	failing bool
}

func (s *campaignConvMediaStub) GetByID(string) (*conversation.ConversationMedia, error) {
	s.t.Error("the campaign attachment was looked up in conversation media, where a library id cannot exist")
	return nil, conversation.ErrMediaNotFound
}

func (s *campaignConvMediaStub) Create(m *conversation.ConversationMedia) error {
	if s.failing {
		return errors.New("insert failed")
	}
	s.created = append(s.created, m)
	return nil
}

type campaignSendAdapter struct {
	entryType shared.EntryType
	requests  []conversation.SendMediaRequest
	err       error
}

func (a *campaignSendAdapter) EntryType() shared.EntryType { return a.entryType }
func (a *campaignSendAdapter) ResolveEntry(_ context.Context, id string) (*conversation.EntryContext, error) {
	return &conversation.EntryContext{EntryID: id, EntryType: a.entryType, WorkspaceID: "ws-1", ContactRef: "5511999999999"}, nil
}
func (a *campaignSendAdapter) WindowState(context.Context, *conversation.EntryContext) (conversation.WindowState, error) {
	return conversation.OpenWindow(nil), nil
}
func (a *campaignSendAdapter) SendText(context.Context, *conversation.EntryContext, conversation.SendTextRequest) (*conversation.SendOutcome, error) {
	return &conversation.SendOutcome{ProviderMessageID: "prov-1"}, nil
}
func (a *campaignSendAdapter) SendMedia(_ context.Context, _ *conversation.EntryContext, req conversation.SendMediaRequest) (*conversation.SendOutcome, error) {
	a.requests = append(a.requests, req)
	if a.err != nil {
		return nil, a.err
	}
	return &conversation.SendOutcome{ProviderMessageID: "prov-1"}, nil
}

type campaignMessageRepo struct {
	conversation.MessageRepository
	created []*conversation.Message
}

func (r *campaignMessageRepo) Create(m *conversation.Message) error {
	r.created = append(r.created, m)
	return nil
}

func newCampaignMediaSender(t *testing.T, library *campaignLibraryStub) (*MessageSenderService, *campaignSendAdapter, *campaignConvMediaStub, *campaignMessageRepo) {
	t.Helper()
	adapter := &campaignSendAdapter{entryType: shared.EntryTypeUnofficialWhatsApp}
	convMedia := &campaignConvMediaStub{t: t}
	msgs := &campaignMessageRepo{}
	svc := &MessageSenderService{
		messageRepo:     msgs,
		mediaRepo:       convMedia,
		channelAdapters: conversation.NewAdapterRegistry(adapter),
	}
	svc.SetMediaLibrary(library)
	return svc, adapter, convMedia, msgs
}

func campaignMediaInput() SendCampaignMessageInput {
	return SendCampaignMessageInput{
		EntryID:     "conv-1",
		EntryType:   string(shared.EntryTypeUnofficialWhatsApp),
		Text:        "confira",
		MediaID:     "lib-1",
		MediaType:   "video",
		FileName:    "promo.mp4",
		WorkspaceID: "ws-1",
	}
}

func TestCampaignMediaIsResolvedFromTheWorkspaceLibrary(t *testing.T) {
	library := &campaignLibraryStub{row: &media.Media{ID: "lib-1", WorkspaceID: "ws-1", URL: "https://cdn/promo.mp4"}}
	svc, adapter, _, _ := newCampaignMediaSender(t, library)

	msg, err := svc.SendCampaignMessage(campaignMediaInput())
	if err != nil {
		t.Fatalf("SendCampaignMessage: %v", err)
	}
	if msg == nil {
		t.Fatal("no message was recorded for a delivered attachment")
	}
	if len(library.ids) != 1 || library.ids[0] != "lib-1" {
		t.Errorf("library consulted with %v, want the campaign's own media id once", library.ids)
	}
	if len(adapter.requests) != 1 {
		t.Fatalf("adapter received %d media sends, want 1", len(adapter.requests))
	}
	req := adapter.requests[0]
	if req.URL != "https://cdn/promo.mp4" {
		t.Errorf("URL = %q, the provider fetches this and it must be the library row's", req.URL)
	}
	// The library row carries no filename; the campaign spec does, and it is
	// what a document renders as on the contact's device.
	if req.FileName != "promo.mp4" {
		t.Errorf("FileName = %q, want the spec's", req.FileName)
	}
	if req.HumanInitiated {
		t.Error("a campaign send must stay non-human-initiated, or it skips the pacing it exists for")
	}
}

// conversation_messages.media_id resolves against conversation media, so writing
// the library id onto the message points the CRM at a row that does not exist:
// the contact gets the file and the operator sees a broken attachment.
func TestCampaignMediaIsBridgedIntoTheConversationForTheTranscript(t *testing.T) {
	library := &campaignLibraryStub{row: &media.Media{ID: "lib-1", WorkspaceID: "ws-1", URL: "https://cdn/promo.mp4"}}
	svc, _, convMedia, msgs := newCampaignMediaSender(t, library)

	if _, err := svc.SendCampaignMessage(campaignMediaInput()); err != nil {
		t.Fatalf("SendCampaignMessage: %v", err)
	}

	if len(convMedia.created) != 1 {
		t.Fatalf("registered %d conversation media rows, want 1", len(convMedia.created))
	}
	row := convMedia.created[0]
	if row.EntryID != "conv-1" || row.URL != "https://cdn/promo.mp4" {
		t.Errorf("row = %+v, want it to point this conversation at the delivered file", row)
	}

	if len(msgs.created) != 1 {
		t.Fatalf("recorded %d messages, want 1", len(msgs.created))
	}
	got := msgs.created[0]
	if got.MediaID == nil {
		t.Fatal("the message carries no media id, so the transcript renders no attachment")
	}
	if *got.MediaID == "lib-1" {
		t.Error("the LIBRARY id was written onto the message; the transcript resolves conversation media and will find nothing")
	}
	if *got.MediaID != row.ID {
		t.Errorf("message media id = %q, want the bridged row %q", *got.MediaID, row.ID)
	}
}

// One bookkeeping failure must not cost a recipient: on a blast that would turn
// a transient insert error into thousands of undelivered messages.
func TestCampaignSendSurvivesAFailedTranscriptBridge(t *testing.T) {
	library := &campaignLibraryStub{row: &media.Media{ID: "lib-1", WorkspaceID: "ws-1", URL: "https://cdn/promo.mp4"}}
	svc, adapter, convMedia, msgs := newCampaignMediaSender(t, library)
	convMedia.failing = true

	if _, err := svc.SendCampaignMessage(campaignMediaInput()); err != nil {
		t.Fatalf("a bookkeeping failure must not fail the send: %v", err)
	}
	if len(adapter.requests) != 1 {
		t.Error("the attachment was not delivered")
	}
	if len(msgs.created) != 1 {
		t.Error("the message was not recorded")
	}
}

// The create endpoint takes a media id straight from the client and never
// validates it, so this is the only thing standing between a campaign and
// another workspace's files.
func TestCampaignMediaFromAnotherWorkspaceIsRefused(t *testing.T) {
	library := &campaignLibraryStub{row: &media.Media{ID: "lib-1", WorkspaceID: "ws-OTHER", URL: "https://cdn/promo.mp4"}}
	svc, adapter, _, _ := newCampaignMediaSender(t, library)

	_, err := svc.SendCampaignMessage(campaignMediaInput())
	if !errors.Is(err, media.ErrMediaNotFound) {
		t.Errorf("err = %v, want it indistinguishable from a missing row so ids cannot be probed across workspaces", err)
	}
	if len(adapter.requests) != 0 {
		t.Error("another workspace's file reached the contact")
	}
}

func TestCampaignMediaMissingFromTheLibraryIsRefused(t *testing.T) {
	for name, library := range map[string]*campaignLibraryStub{
		"no row":     {row: nil},
		"no url":     {row: &media.Media{ID: "lib-1", WorkspaceID: "ws-1"}},
		"read error": {err: errors.New("db down")},
	} {
		t.Run(name, func(t *testing.T) {
			svc, adapter, _, _ := newCampaignMediaSender(t, library)
			if _, err := svc.SendCampaignMessage(campaignMediaInput()); err == nil {
				t.Error("an unresolvable attachment was sent as if it existed")
			}
			if len(adapter.requests) != 0 {
				t.Error("the adapter was called for media that does not resolve")
			}
		})
	}
}

// Nothing about the text and menu payloads changed, and they must not start
// depending on a library that only the media branch needs.
func TestCampaignTextSendDoesNotTouchTheMediaLibrary(t *testing.T) {
	library := &campaignLibraryStub{}
	svc, adapter, convMedia, _ := newCampaignMediaSender(t, library)

	in := campaignMediaInput()
	in.MediaID = ""
	in.MediaType = ""
	if _, err := svc.SendCampaignMessage(in); err != nil {
		t.Fatalf("SendCampaignMessage: %v", err)
	}
	if len(library.ids) != 0 {
		t.Errorf("the library was consulted %d times for a text send", len(library.ids))
	}
	if len(convMedia.created) != 0 {
		t.Error("a text send registered conversation media")
	}
	if len(adapter.requests) != 0 {
		t.Error("a text send went out through SendMedia")
	}
}
