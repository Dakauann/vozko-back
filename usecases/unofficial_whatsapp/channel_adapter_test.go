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
	if sent.DelayMS < uw.MinSendDelayMS {
		t.Errorf("delay = %dms, below the pacing floor", sent.DelayMS)
	}
}

func TestWindowStateDistinguishesItsThreeRefusals(t *testing.T) {
	future := time.Now().UTC().Add(2 * time.Hour)

	t.Run("live session with a reachable contact is window.Open", func(t *testing.T) {
		adapter, _, ec, _ := adapterFixture(t, nil)
		window, err := adapter.WindowState(context.Background(), ec)
		if err != nil || !window.Open {
			t.Fatalf("window.Open = %v, err = %v", window.Open, err)
		}
		if window.ExpiresAt != nil {
			t.Errorf("expiry = %v; this channel has no messaging window", window.ExpiresAt)
		}
	})

	t.Run("dead session closes with no expiry", func(t *testing.T) {
		adapter, _, ec, _ := adapterFixture(t, func(i *uw.Instance, _ *uw.Contact) {
			i.Status = uw.StatusDisconnected
		})
		window, _ := adapter.WindowState(context.Background(), ec)
		if window.Open {
			t.Error("a dead session must close the composer")
		}
		if window.ExpiresAt != nil {
			t.Error("a dead session has no expiry: it needs a reconnect, not a wait")
		}
	})

	t.Run("a WhatsApp restriction closes WITH an expiry", func(t *testing.T) {
		adapter, _, ec, _ := adapterFixture(t, func(i *uw.Instance, _ *uw.Contact) {
			i.Restriction = uw.Restriction{Until: &future}
		})
		window, _ := adapter.WindowState(context.Background(), ec)
		if window.Open {
			t.Error("a restricted number must not send")
		}
		if window.ExpiresAt == nil {
			t.Fatal("a restriction has an expiry, and the UI needs it to say when it lifts")
		}
	})

	t.Run("a blocked contact closes with no expiry", func(t *testing.T) {
		adapter, _, ec, _ := adapterFixture(t, func(_ *uw.Instance, c *uw.Contact) {
			c.Blocked = true
		})
		window, _ := adapter.WindowState(context.Background(), ec)
		if window.Open {
			t.Error("a blocked contact must close the composer")
		}
		if window.ExpiresAt != nil {
			t.Error("being blocked has no expiry: nothing we do reopens it")
		}
	})
}

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

	cached, _ := adapter.WindowState(context.Background(), ec)
	if cached.Open {
		t.Error("the restriction was not cached; the next send would hit the provider again")
	}
	if cached.Reason != conversation.WindowReasonAccountRestricted {
		t.Errorf("reason = %q, want account_restricted", cached.Reason)
	}
}

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

func TestAutomatedSendsStayPacedByDefault(t *testing.T) {
	adapter, messaging, ec, _ := adapterFixture(t, nil)

	if _, err := adapter.SendText(context.Background(), ec,
		conversation.SendTextRequest{Body: "resposta automática"}); err != nil {
		t.Fatalf("SendText: %v", err)
	}

	if got := messaging.texts[0].DelayMS; got < uw.MinSendDelayMS {
		t.Errorf("delay = %dms, want at least the %dms floor", got, uw.MinSendDelayMS)
	}
}

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
