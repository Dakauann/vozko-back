package conversation_usecase

import (
	"context"
	"errors"
	"testing"

	"vozko/domain/conversation"
	lmw "vozko/domain/lead_message_window"
)

type reactionCapturingClient struct {
	conversation.WhatsAppClient
	reactions []string
}

func (c *reactionCapturingClient) SendReaction(_ context.Context, to, messageID, emoji string) error {
	c.reactions = append(c.reactions, to+"|"+messageID+"|"+emoji)
	return nil
}

type closedWindowRepo struct{ lmw.Repository }

func (closedWindowRepo) IsWindowOpen(string, string) (bool, error) { return false, nil }

func officialReactionSender(client conversation.WhatsAppClient, windows lmw.Repository) *MessageSenderService {
	return &MessageSenderService{
		whatsappRepo:          stubAudioEntryRepo{},
		leadRepo:              stubAudioLeadRepo{},
		messageWindowRepo:     windows,
		whatsappClientFactory: stubAudioClientFactory{client: client},
	}
}

func TestOfficialWhatsAppReactsThroughTheCloudAPI(t *testing.T) {
	client := &reactionCapturingClient{}
	channel := officialReactionSender(client, stubAudioWindowRepo{}).OfficialReactions()

	if got := conversation.MessageCapabilitiesOf(channel); got != (conversation.MessageCapabilities{React: true}) {
		t.Fatalf("capabilities = %+v, want react only: the Cloud API cannot edit or delete", got)
	}
	ec, err := channel.ResolveEntry(context.Background(), "entry-1")
	if err != nil {
		t.Fatal(err)
	}
	reacting := channel.(conversation.ReactingAdapter)
	if err := reacting.SendReaction(context.Background(), ec, "wamid.target", "👍"); err != nil {
		t.Fatal(err)
	}
	if err := reacting.RemoveReaction(context.Background(), ec, "wamid.target"); err != nil {
		t.Fatal(err)
	}
	if len(client.reactions) != 2 || client.reactions[0] != ec.ContactRef+"|wamid.target|👍" || client.reactions[1] != ec.ContactRef+"|wamid.target|" {
		t.Fatalf("reactions = %v", client.reactions)
	}
}

func TestOfficialWhatsAppDoesNotReactOutsideTheWindow(t *testing.T) {
	client := &reactionCapturingClient{}
	channel := officialReactionSender(client, closedWindowRepo{}).OfficialReactions()
	ec, err := channel.ResolveEntry(context.Background(), "entry-1")
	if err != nil {
		t.Fatal(err)
	}

	err = channel.(conversation.ReactingAdapter).SendReaction(context.Background(), ec, "wamid.target", "👍")
	if !errors.Is(err, conversation.ErrOutboundWindowClosed) {
		t.Fatalf("err = %v, want ErrOutboundWindowClosed", err)
	}
	if len(client.reactions) != 0 {
		t.Fatal("a reaction went out with the window closed")
	}
}

func TestOfficialWhatsAppReactionNeedsAClientThatCanReact(t *testing.T) {
	channel := officialReactionSender(&buttonCapturingClient{}, stubAudioWindowRepo{}).OfficialReactions()
	ec, err := channel.ResolveEntry(context.Background(), "entry-1")
	if err != nil {
		t.Fatal(err)
	}
	err = channel.(conversation.ReactingAdapter).SendReaction(context.Background(), ec, "wamid.target", "👍")
	if !errors.Is(err, conversation.ErrCapabilityUnsupported) {
		t.Fatalf("err = %v, want ErrCapabilityUnsupported", err)
	}
}
