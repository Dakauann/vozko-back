package unofficial_whatsapp

import (
	"context"
	"errors"
	"strings"
	"testing"
	"time"

	"vozko/domain/conversation"
	uw "vozko/domain/unofficial_whatsapp"
)

func adapterFixture(t *testing.T, mutate func(*uw.Instance, *uw.Contact)) (
	conversation.ChannelAdapter, *fakeMessaging, *conversation.EntryContext, *fakeInstanceRepo,
) {
	t.Helper()

	instance := &uw.Instance{
		ID: "inst-1", WorkspaceID: "ws-1", ServerID: "srv-a",
		Status: uw.StatusConnected, InstanceToken: "tok",
		PhoneNumber: "5511777777777", SendDelayMinMS: 3000, SendDelayMaxMS: 12000,
	}
	contact := &uw.Contact{
		ID: "contact-1", WorkspaceID: "ws-1", InstanceID: "inst-1",
		JID: "5511999999999@s.whatsapp.net", PhoneNumber: "5511999999999",
	}
	if mutate != nil {
		mutate(instance, contact)
	}

	conv := &uw.Conversation{
		ID: "conv-1", WorkspaceID: "ws-1", InstanceID: "inst-1",
		ContactID: "contact-1", ChatID: contact.JID,
	}

	instances := newFakeInstanceRepo(instance)
	messaging := &fakeMessaging{}
	adapter := NewChannelAdapter(
		instances,
		newFakeServerRepo(healthyServer("srv-a", 10, 1)),
		newFakeContactRepo(contact),
		newFakeConversationRepo(conv),
		messaging,
	)

	ec := &conversation.EntryContext{
		EntryID: "conv-1", WorkspaceID: "ws-1",
		AccountID: "inst-1", ContactID: "contact-1", ContactRef: conv.ChatID,
	}
	return adapter, messaging, ec, instances
}

// The single most dangerous data bug this channel can have: the provider
// substitutes {{...}} from ITS lead store, which is not ours, so an unescaped
// placeholder leaks another record's data into a customer's chat.
func TestSendTextNeutralisesProviderPlaceholders(t *testing.T) {
	adapter, messaging, ec, _ := adapterFixture(t, nil)

	_, err := adapter.SendText(context.Background(), ec,
		conversation.SendTextRequest{Body: "Olá {{name}}, seu boleto venceu"})
	if err != nil {
		t.Fatalf("SendText: %v", err)
	}

	sent := messaging.texts[0].Text
	if strings.Contains(sent, "{{name}}") {
		t.Fatalf("the provider's placeholder reached the wire: %q", sent)
	}
	if !strings.Contains(sent, "boleto venceu") {
		t.Errorf("the operator's own words were altered: %q", sent)
	}
}

// The correlation tag is what makes the echo recognisable. Without it, our own
// send comes back looking like the owner typed it on their phone.
func TestSendTextStampsTheEchoTag(t *testing.T) {
	adapter, messaging, ec, _ := adapterFixture(t, nil)

	if _, err := adapter.SendText(context.Background(), ec,
		conversation.SendTextRequest{Body: "oi"}); err != nil {
		t.Fatalf("SendText: %v", err)
	}

	sent := messaging.texts[0]
	if sent.TrackSource != uw.TrackSource || sent.TrackID != ec.EntryID {
		t.Errorf("echo tag = %q/%q, want %q/%q",
			sent.TrackSource, sent.TrackID, uw.TrackSource, ec.EntryID)
	}
	// Paced, because this request is not marked human-initiated: an automated
	// burst of instant replies is one of the signals that gets a number banned.
	if sent.DelayMS < uw.MinSendDelayMS {
		t.Errorf("delay = %dms, below the pacing floor", sent.DelayMS)
	}
}

// Three distinct things close the composer, each needing different words and a
// different remedy. Only one of them has an expiry to count down.
func TestWindowStateDistinguishesItsThreeRefusals(t *testing.T) {
	future := time.Now().UTC().Add(2 * time.Hour)

	t.Run("live session with a reachable contact is open", func(t *testing.T) {
		adapter, _, ec, _ := adapterFixture(t, nil)
		open, expires, err := adapter.WindowState(context.Background(), ec)
		if err != nil || !open {
			t.Fatalf("open = %v, err = %v", open, err)
		}
		// No clock on this channel: an expiry here would make the UI render a
		// countdown that means nothing.
		if expires != nil {
			t.Errorf("expiry = %v; this channel has no messaging window", expires)
		}
	})

	t.Run("dead session closes with no expiry", func(t *testing.T) {
		adapter, _, ec, _ := adapterFixture(t, func(i *uw.Instance, _ *uw.Contact) {
			i.Status = uw.StatusDisconnected
		})
		open, expires, _ := adapter.WindowState(context.Background(), ec)
		if open {
			t.Error("a dead session must close the composer")
		}
		if expires != nil {
			t.Error("a dead session has no expiry: it needs a reconnect, not a wait")
		}
	})

	t.Run("a WhatsApp restriction closes WITH an expiry", func(t *testing.T) {
		adapter, _, ec, _ := adapterFixture(t, func(i *uw.Instance, _ *uw.Contact) {
			i.Restriction = uw.Restriction{Until: &future}
		})
		open, expires, _ := adapter.WindowState(context.Background(), ec)
		if open {
			t.Error("a restricted number must not send")
		}
		if expires == nil {
			t.Fatal("a restriction has an expiry, and the UI needs it to say when it lifts")
		}
	})

	t.Run("a blocked contact closes with no expiry", func(t *testing.T) {
		adapter, _, ec, _ := adapterFixture(t, func(_ *uw.Instance, c *uw.Contact) {
			c.Blocked = true
		})
		open, expires, _ := adapter.WindowState(context.Background(), ec)
		if open {
			t.Error("a blocked contact must close the composer")
		}
		if expires != nil {
			t.Error("being blocked has no expiry: nothing we do reopens it")
		}
	})
}

// The send path must refuse before reaching the provider, for the same reasons
// the composer does — otherwise a workflow keeps blasting a number WhatsApp has
// already begun limiting.
func TestSendRefusesWhenTheInstanceCannotSend(t *testing.T) {
	future := time.Now().UTC().Add(time.Hour)

	cases := []struct {
		name    string
		mutate  func(*uw.Instance, *uw.Contact)
		wantErr error
	}{
		{"dead session", func(i *uw.Instance, _ *uw.Contact) {
			i.Status = uw.StatusDisconnected
		}, uw.ErrInstanceNotConnected},
		{"restricted number", func(i *uw.Instance, _ *uw.Contact) {
			i.Restriction = uw.Restriction{Until: &future}
		}, uw.ErrRestrictedByWA},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			adapter, messaging, ec, _ := adapterFixture(t, tc.mutate)
			_, err := adapter.SendText(context.Background(), ec,
				conversation.SendTextRequest{Body: "oi"})
			if !errors.Is(err, tc.wantErr) {
				t.Fatalf("err = %v, want %v", err, tc.wantErr)
			}
			if len(messaging.texts) != 0 {
				t.Error("no byte may reach the provider for a refused send")
			}
		})
	}
}

// A restriction seen once must be cached on the instance, so every later send —
// including a running broadcast — is refused before reaching the provider.
// Learning it once per message is how a limited number becomes a banned one.
func TestSendCachesAWhatsAppRestriction(t *testing.T) {
	adapter, messaging, ec, _ := adapterFixture(t, nil)
	blocked := false
	messaging.SendTextFn = func(context.Context, uw.InstanceRef, uw.SendTextInput) (*uw.SendResult, error) {
		return nil, &uw.ProviderError{
			HTTPStatus: 400, ProviderCode: 463,
			Restriction: &uw.Restriction{CanSendNewChats: &blocked},
		}
	}

	if _, err := adapter.SendText(context.Background(), ec,
		conversation.SendTextRequest{Body: "oi"}); err == nil {
		t.Fatal("a restriction must surface as an error")
	}

	// The next window check must already know, without another provider call.
	open, _, _ := adapter.WindowState(context.Background(), ec)
	if open {
		t.Error("the restriction was not cached; the next send would hit the provider again")
	}
}

// The adapter applies WhatsApp's own caps rather than trusting the caller: an
// author's option list is unbounded upstream, and the provider truncates
// silently with no error.
func TestSendInteractiveAppliesWhatsAppsCaps(t *testing.T) {
	t.Run("more than three options becomes a list", func(t *testing.T) {
		adapter, messaging, ec, _ := adapterFixture(t, nil)
		options := make([]conversation.InteractiveOption, 0, 8)
		for i := 0; i < 8; i++ {
			options = append(options, conversation.InteractiveOption{
				ID: string(rune('a' + i)), Title: "opção",
			})
		}

		_, err := adapter.(conversation.InteractiveAdapter).SendInteractive(
			context.Background(), ec,
			conversation.SendInteractiveRequest{Body: "escolha", Style: "buttons", Options: options})
		if err != nil {
			t.Fatalf("SendInteractive: %v", err)
		}

		menu := messaging.menus[0]
		if menu.Style != uw.InteractiveStyleList {
			t.Errorf("style = %q; eight options cannot render as buttons", menu.Style)
		}
		if len(menu.Options) > uw.MaxListOptions {
			t.Errorf("%d options sent, WhatsApp renders at most %d", len(menu.Options), uw.MaxListOptions)
		}
	})

	t.Run("three or fewer stay buttons", func(t *testing.T) {
		adapter, messaging, ec, _ := adapterFixture(t, nil)
		_, err := adapter.(conversation.InteractiveAdapter).SendInteractive(
			context.Background(), ec, conversation.SendInteractiveRequest{
				Body:  "escolha",
				Style: "buttons",
				Options: []conversation.InteractiveOption{
					{ID: "a", Title: "Sim"}, {ID: "b", Title: "Não"},
				},
			})
		if err != nil {
			t.Fatalf("SendInteractive: %v", err)
		}
		if messaging.menus[0].Style != uw.InteractiveStyleButtons {
			t.Errorf("style = %q, want buttons", messaging.menus[0].Style)
		}
	})
}

// Every optional capability is discovered by type assertion. A method that
// drifts out of shape makes the channel silently lose the feature, so the
// assertions are pinned here as well as at compile time.
func TestAdapterImplementsEveryClaimedCapability(t *testing.T) {
	adapter, _, _, _ := adapterFixture(t, nil)

	if _, ok := adapter.(conversation.ReactingAdapter); !ok {
		t.Error("reactions are claimed by the descriptor but not implemented")
	}
	if _, ok := adapter.(conversation.PresenceAdapter); !ok {
		t.Error("typing indicators are claimed but not implemented")
	}
	if _, ok := adapter.(conversation.EditingAdapter); !ok {
		t.Error("message editing is claimed but not implemented")
	}
	if _, ok := adapter.(conversation.RetractingAdapter); !ok {
		t.Error("unsend is claimed but not implemented")
	}
	if _, ok := adapter.(conversation.InteractiveAdapter); !ok {
		t.Error("interactive prompts are claimed but not implemented")
	}
}

// An empty emoji is the provider's documented REMOVAL, which is why remove and
// set are the same call with a different argument.
func TestRemoveReactionSendsAnEmptyEmoji(t *testing.T) {
	adapter, messaging, ec, _ := adapterFixture(t, nil)
	reacting := adapter.(conversation.ReactingAdapter)

	if err := reacting.RemoveReaction(context.Background(), ec, "pm-1"); err != nil {
		t.Fatalf("RemoveReaction: %v", err)
	}
	if len(messaging.reacts) != 1 || messaging.reacts[0] != "pm-1:" {
		t.Errorf("reacts = %v, want an empty emoji for removal", messaging.reacts)
	}
}

// Editing must sanitise too: a corrected message goes through the same provider
// substitution as the original.
func TestEditTextSanitises(t *testing.T) {
	adapter, messaging, ec, _ := adapterFixture(t, nil)
	editing := adapter.(conversation.EditingAdapter)

	if err := editing.EditText(context.Background(), ec, "pm-1", "corrigido {{name}}"); err != nil {
		t.Fatalf("EditText: %v", err)
	}
	if strings.Contains(messaging.edits[0], "{{name}}") {
		t.Errorf("an edit reached the wire unsanitised: %q", messaging.edits[0])
	}
}

// A body over the cap must be refused locally rather than by the provider, so
// the operator gets an error they can act on.
func TestSendRefusesAnOversizedBody(t *testing.T) {
	adapter, messaging, ec, _ := adapterFixture(t, nil)
	huge := strings.Repeat("a", uw.MaxTextRunes+1)

	if _, err := adapter.SendText(context.Background(), ec,
		conversation.SendTextRequest{Body: huge}); !errors.Is(err, uw.ErrTextTooLong) {
		t.Fatalf("err = %v, want ErrTextTooLong", err)
	}
	if len(messaging.texts) != 0 {
		t.Error("an oversized body must not reach the provider")
	}
}

// ResolveEntry must address the CHAT, not the contact's own JID: they are equal
// in a private chat but diverge for a group, and using the participant's JID
// would send a group reply to one member.
func TestResolveEntryAddressesTheChat(t *testing.T) {
	adapter, _, _, _ := adapterFixture(t, nil)

	ec, err := adapter.ResolveEntry(context.Background(), "conv-1")
	if err != nil {
		t.Fatalf("ResolveEntry: %v", err)
	}
	if ec.ContactRef != "5511999999999@s.whatsapp.net" {
		t.Errorf("contactRef = %q, want the chat id", ec.ContactRef)
	}
	if ec.AccountID != "inst-1" {
		t.Errorf("accountID = %q; a reply must leave from the number it arrived on", ec.AccountID)
	}
}

// An operator's own message must not be paced.
//
// Regression test for a live complaint that messages took "a lot of time" to
// send. The delay is not a local wait: it is handed to the host, which renders
// "Digitando…" for its full duration BEFORE dispatching. With the instance at
// its default 3-12s range, every operator reply sat for up to twelve seconds
// while the operator watched. Pacing exists to make MACHINE traffic look human;
// a human reply is the thing it is imitating, so it buys nothing here.
func TestOperatorSendsAreNotPaced(t *testing.T) {
	adapter, messaging, ec, _ := adapterFixture(t, nil)

	if _, err := adapter.SendText(context.Background(), ec,
		conversation.SendTextRequest{Body: "já verifico para você", HumanInitiated: true}); err != nil {
		t.Fatalf("SendText: %v", err)
	}

	if got := messaging.texts[0].DelayMS; got != 0 {
		t.Errorf("delay = %dms on an operator send, want 0", got)
	}
}

// The exemption is opt-IN, so a caller that forgets it is merely slow rather
// than unpaced. On a channel where looking automated costs the customer their
// number, that is the only safe direction for the default to fail.
func TestAutomatedSendsStayPacedByDefault(t *testing.T) {
	adapter, messaging, ec, _ := adapterFixture(t, nil)

	// An AI reply: constructed without naming the field at all.
	if _, err := adapter.SendText(context.Background(), ec,
		conversation.SendTextRequest{Body: "resposta automática"}); err != nil {
		t.Fatalf("SendText: %v", err)
	}

	if got := messaging.texts[0].DelayMS; got < uw.MinSendDelayMS {
		t.Errorf("delay = %dms, want at least the %dms floor", got, uw.MinSendDelayMS)
	}
}

// Media follows the same rule: an operator attaching a file waits no longer
// than an operator typing.
func TestOperatorMediaSendsAreNotPaced(t *testing.T) {
	adapter, messaging, ec, _ := adapterFixture(t, nil)

	_, err := adapter.SendMedia(context.Background(), ec, conversation.SendMediaRequest{
		Kind: "image", URL: "https://example.com/a.jpg", MIMEType: "image/jpeg",
		HumanInitiated: true,
	})
	if err != nil {
		t.Fatalf("SendMedia: %v", err)
	}
	if len(messaging.media) == 0 {
		t.Fatal("no media was sent")
	}
	if got := messaging.media[0].DelayMS; got != 0 {
		t.Errorf("delay = %dms on an operator media send, want 0", got)
	}
}

// A recording made in the CRM must send.
//
// Regression test for a live failure: "unofficial whatsapp: audio/wav is not
// accepted for audio media". The composer records opus in the browser and
// transcodes to WAV so the waveform and playback work everywhere, so WAV is the
// ONE format every voice note in this product actually has — and the descriptor
// mirrored WhatsApp's published accept-list, which excludes it. The send path
// now converts instead of refusing, exactly as the official WhatsApp path
// always has.
type fakeVoiceTranscoder struct {
	calledWith string
	out        []byte
	err        error
}

func (f *fakeVoiceTranscoder) ToVoiceNote(_ context.Context, url string) ([]byte, error) {
	f.calledWith = url
	if f.err != nil {
		return nil, f.err
	}
	if f.out == nil {
		f.out = []byte("OggS-fake")
	}
	return f.out, nil
}

func TestWavRecordingIsConvertedRatherThanRefused(t *testing.T) {
	adapter, messaging, ec, _ := adapterFixture(t, nil)
	voice := &fakeVoiceTranscoder{}
	adapter.(interface {
		SetVoiceTranscoder(uw.VoiceTranscoder)
	}).SetVoiceTranscoder(voice)

	_, err := adapter.SendMedia(context.Background(), ec, conversation.SendMediaRequest{
		Kind: "audio", URL: "https://media.example.com/a.wav",
		MIMEType: "audio/wav", FileName: "audio-message.wav", HumanInitiated: true,
	})
	if err != nil {
		t.Fatalf("SendMedia: %v", err)
	}

	if voice.calledWith != "https://media.example.com/a.wav" {
		t.Errorf("transcoder was handed %q", voice.calledWith)
	}
	if len(messaging.media) != 1 {
		t.Fatalf("expected one media send, got %d", len(messaging.media))
	}
	sent := messaging.media[0]
	// What reaches the provider must be the CONVERTED file, never the WAV: the
	// whole point is that WhatsApp would not take the original.
	if sent.MIMEType != "audio/ogg" {
		t.Errorf("mime = %q, want audio/ogg", sent.MIMEType)
	}
	if sent.Kind != uw.MediaVoice {
		t.Errorf("kind = %q, want a voice note", sent.Kind)
	}
	if sent.Base64 == "" {
		t.Error("converted bytes were not sent")
	}
	if sent.URL != "" {
		t.Errorf("the original url survived (%q); the provider would fetch the WAV", sent.URL)
	}
}

// Without a transcoder the failure names itself, rather than the channel
// refusing to start or the audio silently going out unconverted.
func TestAudioWithoutATranscoderFailsWithAReason(t *testing.T) {
	adapter, _, ec, _ := adapterFixture(t, nil)

	_, err := adapter.SendMedia(context.Background(), ec, conversation.SendMediaRequest{
		Kind: "audio", URL: "https://media.example.com/a.wav", MIMEType: "audio/wav",
	})
	if err == nil {
		t.Fatal("audio was accepted with no transcoder configured")
	}
	if !strings.Contains(err.Error(), "transcoder") {
		t.Errorf("err = %q; it must name the missing piece", err)
	}
}
