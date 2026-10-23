package emailsending

import (
	"testing"
	"time"
)

func at(min int) time.Time {
	return time.Date(2026, 10, 21, 12, min, 0, 0, time.UTC)
}

func TestApplyEvent(t *testing.T) {
	cases := []struct {
		name  string
		start Message
		event DeliveryEvent
		check func(t *testing.T, m Message, effect EventEffect)
	}{
		{
			name:  "delivery stamps delivered once",
			start: Message{Status: MessageSent},
			event: DeliveryEvent{Kind: EventDelivery, OccurredAt: at(1)},
			check: func(t *testing.T, m Message, e EventEffect) {
				if m.Status != MessageDelivered || m.DeliveredAt == nil || !m.DeliveredAt.Equal(at(1)) || !e.Changed {
					t.Fatalf("got %+v %+v", m, e)
				}
			},
		},
		{
			name:  "human open stamps first open and counts",
			start: Message{Status: MessageDelivered},
			event: DeliveryEvent{Kind: EventOpen, OccurredAt: at(2)},
			check: func(t *testing.T, m Message, e EventEffect) {
				if m.FirstOpenAt == nil || m.Opens != 1 || !e.FirstOpen {
					t.Fatalf("got %+v %+v", m, e)
				}
			},
		},
		{
			name:  "second open keeps the first timestamp",
			start: Message{Status: MessageDelivered, FirstOpenAt: ptr(at(2)), Opens: 1},
			event: DeliveryEvent{Kind: EventOpen, OccurredAt: at(5)},
			check: func(t *testing.T, m Message, e EventEffect) {
				if !m.FirstOpenAt.Equal(at(2)) || m.Opens != 2 || e.FirstOpen {
					t.Fatalf("got %+v %+v", m, e)
				}
			},
		},
		{
			name:  "bot open is counted apart and never stamps first open",
			start: Message{Status: MessageDelivered},
			event: DeliveryEvent{Kind: EventOpen, OccurredAt: at(2), IsBot: true},
			check: func(t *testing.T, m Message, e EventEffect) {
				if m.FirstOpenAt != nil || m.Opens != 0 || m.BotOpens != 1 || e.FirstOpen {
					t.Fatalf("got %+v %+v", m, e)
				}
			},
		},
		{
			name:  "human click also implies an open",
			start: Message{Status: MessageDelivered},
			event: DeliveryEvent{Kind: EventClick, OccurredAt: at(3), Link: "https://loja.com.br"},
			check: func(t *testing.T, m Message, e EventEffect) {
				if m.FirstClickAt == nil || m.Clicks != 1 || m.FirstOpenAt == nil || !e.FirstClick || !e.FirstOpen {
					t.Fatalf("got %+v %+v", m, e)
				}
			},
		},
		{
			name:  "bot click is ignored for engagement",
			start: Message{Status: MessageDelivered},
			event: DeliveryEvent{Kind: EventClick, OccurredAt: at(3), Link: "https://loja.com.br", IsBot: true},
			check: func(t *testing.T, m Message, e EventEffect) {
				if m.FirstClickAt != nil || m.Clicks != 0 || e.FirstClick {
					t.Fatalf("got %+v %+v", m, e)
				}
			},
		},
		{
			name:  "hard bounce marks bounced and asks for suppression",
			start: Message{Status: MessageSent},
			event: DeliveryEvent{Kind: EventBounce, OccurredAt: at(1), Bounce: &Bounce{Kind: BounceHard, SubType: "NoEmail"}},
			check: func(t *testing.T, m Message, e EventEffect) {
				if m.Status != MessageBounced || m.BounceKind != BounceHard || !e.Suppress || e.SuppressReason != SuppressHardBounce {
					t.Fatalf("got %+v %+v", m, e)
				}
			},
		},
		{
			name:  "soft bounce marks bounced without suppression",
			start: Message{Status: MessageSent},
			event: DeliveryEvent{Kind: EventBounce, OccurredAt: at(1), Bounce: &Bounce{Kind: BounceSoft, SubType: "MailboxFull"}},
			check: func(t *testing.T, m Message, e EventEffect) {
				if m.Status != MessageBounced || m.BounceKind != BounceSoft || e.Suppress {
					t.Fatalf("got %+v %+v", m, e)
				}
			},
		},
		{
			name:  "suppressed bounce is not a reputation bounce",
			start: Message{Status: MessageSent},
			event: DeliveryEvent{Kind: EventBounce, OccurredAt: at(1), Bounce: &Bounce{Kind: BounceSuppressed, SubType: "OnTenantSuppressionList"}},
			check: func(t *testing.T, m Message, e EventEffect) {
				if m.Status != MessageSuppressed || e.Suppress {
					t.Fatalf("got %+v %+v", m, e)
				}
			},
		},
		{
			name:  "complaint suppresses and unsubscribes",
			start: Message{Status: MessageDelivered},
			event: DeliveryEvent{Kind: EventComplaint, OccurredAt: at(9)},
			check: func(t *testing.T, m Message, e EventEffect) {
				if m.ComplainedAt == nil || !e.Suppress || e.SuppressReason != SuppressComplaint || !e.Unsubscribe {
					t.Fatalf("got %+v %+v", m, e)
				}
			},
		},
		{
			name:  "not spam feedback is not a complaint",
			start: Message{Status: MessageDelivered},
			event: DeliveryEvent{Kind: EventComplaint, OccurredAt: at(9), Complaint: &Complaint{FeedbackType: "not-spam"}},
			check: func(t *testing.T, m Message, e EventEffect) {
				if m.ComplainedAt != nil || e.Suppress || e.Changed {
					t.Fatalf("got %+v %+v", m, e)
				}
			},
		},
		{
			name:  "late delivery after a bounce does not resurrect the message",
			start: Message{Status: MessageBounced, BounceKind: BounceHard, BouncedAt: ptr(at(1))},
			event: DeliveryEvent{Kind: EventDelivery, OccurredAt: at(2)},
			check: func(t *testing.T, m Message, e EventEffect) {
				if m.Status != MessageBounced {
					t.Fatalf("got %+v", m)
				}
			},
		},
		{
			name:  "reject marks failed",
			start: Message{Status: MessageSent},
			event: DeliveryEvent{Kind: EventReject, OccurredAt: at(1)},
			check: func(t *testing.T, m Message, e EventEffect) {
				if m.Status != MessageFailed {
					t.Fatalf("got %+v", m)
				}
			},
		},
		{
			name:  "rendering failure marks failed",
			start: Message{Status: MessageSent},
			event: DeliveryEvent{Kind: EventRenderingFailure, OccurredAt: at(1)},
			check: func(t *testing.T, m Message, e EventEffect) {
				if m.Status != MessageFailed {
					t.Fatalf("got %+v", m)
				}
			},
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			m := tc.start
			effect := ApplyEvent(&m, tc.event)
			tc.check(t, m, effect)
		})
	}
}

func TestBounceKindOf(t *testing.T) {
	cases := []struct {
		bounceType, subType string
		want                BounceKind
	}{
		{bounceType: "Permanent", subType: "General", want: BounceHard},
		{bounceType: "Permanent", subType: "NoEmail", want: BounceHard},
		{bounceType: "Permanent", subType: "Suppressed", want: BounceSuppressed},
		{bounceType: "Permanent", subType: "OnAccountSuppressionList", want: BounceSuppressed},
		{bounceType: "Permanent", subType: "OnTenantSuppressionList", want: BounceSuppressed},
		{bounceType: "Permanent", subType: "EmailValidationSuppressed", want: BounceSuppressed},
		{bounceType: "Transient", subType: "MailboxFull", want: BounceSoft},
		{bounceType: "Undetermined", subType: "Undetermined", want: BounceSoft},
		{bounceType: "", subType: "", want: BounceSoft},
	}
	for _, tc := range cases {
		if got := BounceKindOf(tc.bounceType, tc.subType); got != tc.want {
			t.Fatalf("BounceKindOf(%q, %q) = %q, want %q", tc.bounceType, tc.subType, got, tc.want)
		}
	}
}

func TestListUnsubscribeHeaders(t *testing.T) {
	headers, err := ListUnsubscribeHeaders("https://api.test/public/email/unsubscribe/tok", "unsubscribe+tok@joao.vozkomail.com.br")
	if err != nil {
		t.Fatal(err)
	}
	want := map[string]string{
		"List-Unsubscribe":      "<https://api.test/public/email/unsubscribe/tok>, <mailto:unsubscribe+tok@joao.vozkomail.com.br>",
		"List-Unsubscribe-Post": "List-Unsubscribe=One-Click",
	}
	if len(headers) != 2 {
		t.Fatalf("headers = %v", headers)
	}
	for _, h := range headers {
		if want[h.Name] != h.Value {
			t.Fatalf("header %s = %q, want %q", h.Name, h.Value, want[h.Name])
		}
	}
	if _, err := ListUnsubscribeHeaders("http://insecure", ""); err == nil {
		t.Fatal("one click needs an https url")
	}
}

func TestOutboundMessageValidate(t *testing.T) {
	ok := OutboundMessage{
		TenantName: "ws-1",
		From:       Mailbox{Name: "Loja", Address: "contato@loja.com.br"},
		To:         "ana@cliente.com",
		Subject:    "Oi",
		HTML:       "<p>Oi</p>",
		Text:       "Oi",
		Kind:       KindTransactional,
	}
	if err := ok.Validate(); err != nil {
		t.Fatalf("valid message = %v", err)
	}
	marketing := ok
	marketing.Kind = KindMarketing
	if err := marketing.Validate(); err == nil {
		t.Fatal("marketing without List-Unsubscribe must fail")
	}
	headers, _ := ListUnsubscribeHeaders("https://api.test/u/t", "")
	marketing.Headers = headers
	if err := marketing.Validate(); err != nil {
		t.Fatalf("marketing with headers = %v", err)
	}
	mutations := []func(*OutboundMessage){
		func(m *OutboundMessage) { m.TenantName = "" },
		func(m *OutboundMessage) { m.From.Address = "" },
		func(m *OutboundMessage) { m.From.Address = "joao@gmail.com" },
		func(m *OutboundMessage) { m.To = "" },
		func(m *OutboundMessage) { m.Subject = "" },
		func(m *OutboundMessage) { m.Subject = "a\r\nBcc: x" },
		func(m *OutboundMessage) { m.HTML = ""; m.Text = "" },
		func(m *OutboundMessage) { m.Kind = "" },
		func(m *OutboundMessage) { m.Headers = []Header{{Name: "X-Bad", Value: "a\nb"}} },
	}
	for i, mutate := range mutations {
		m := ok
		mutate(&m)
		if err := m.Validate(); err == nil {
			t.Fatalf("mutation %d should fail validation", i)
		}
	}
}

func ptr(t time.Time) *time.Time { return &t }

func TestUnsubscribedRecipientIsAListHitNotADeadMailbox(t *testing.T) {
	if got := BounceKindOf("Permanent", "UnsubscribedRecipient"); got != BounceSuppressed {
		t.Fatalf("BounceKindOf(UnsubscribedRecipient) = %q, want %q so the shared bad-address list never takes a live address", got, BounceSuppressed)
	}
}

func TestApplyEventFlagsFirstOutcomesOnceForTheGateCounters(t *testing.T) {
	cases := []struct {
		name  string
		start Message
		event DeliveryEvent
		want  EventEffect
	}{
		{
			name:  "the first delivery is counted",
			start: Message{Status: MessageSent},
			event: DeliveryEvent{Kind: EventDelivery, OccurredAt: at(1)},
			want:  EventEffect{Changed: true, Delivered: true},
		},
		{
			name:  "a second delivery is not counted again",
			start: Message{Status: MessageDelivered, DeliveredAt: ptr(at(1))},
			event: DeliveryEvent{Kind: EventDelivery, OccurredAt: at(2)},
			want:  EventEffect{},
		},
		{
			name:  "the first hard bounce is counted",
			start: Message{Status: MessageSent},
			event: DeliveryEvent{Kind: EventBounce, OccurredAt: at(1), Bounce: &Bounce{Kind: BounceHard, SubType: "General"}},
			want:  EventEffect{Changed: true, Suppress: true, SuppressReason: SuppressHardBounce, HardBounced: true},
		},
		{
			name:  "a second hard bounce of the same message is not counted again",
			start: Message{Status: MessageBounced, BounceKind: BounceHard, BouncedAt: ptr(at(1))},
			event: DeliveryEvent{Kind: EventBounce, OccurredAt: at(3), Bounce: &Bounce{Kind: BounceHard, SubType: "General"}},
			want:  EventEffect{Changed: true, Suppress: true, SuppressReason: SuppressHardBounce},
		},
		{
			name:  "the first complaint is counted",
			start: Message{Status: MessageDelivered},
			event: DeliveryEvent{Kind: EventComplaint, OccurredAt: at(9)},
			want:  EventEffect{Changed: true, Suppress: true, SuppressReason: SuppressComplaint, Unsubscribe: true, Complained: true},
		},
		{
			name:  "a second complaint is not counted again",
			start: Message{Status: MessageDelivered, ComplainedAt: ptr(at(9))},
			event: DeliveryEvent{Kind: EventComplaint, OccurredAt: at(10)},
			want:  EventEffect{Changed: true, Suppress: true, SuppressReason: SuppressComplaint, Unsubscribe: true},
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			m := tc.start
			if got := ApplyEvent(&m, tc.event); got != tc.want {
				t.Fatalf("effect = %+v, want %+v", got, tc.want)
			}
		})
	}
}

func TestSuppressReasonsStoredAndRemovable(t *testing.T) {
	stored := map[SuppressReason]bool{SuppressHardBounce: true, SuppressComplaint: true, SuppressManual: true, SuppressSoftBounce: true}
	removable := map[SuppressReason]bool{SuppressManual: true, SuppressSoftBounce: true}
	for _, r := range []SuppressReason{SuppressHardBounce, SuppressComplaint, SuppressManual, SuppressSoftBounce, SuppressBadAddress, SuppressNoMailDomain} {
		if r.Stored() != stored[r] {
			t.Fatalf("%s.Stored = %v", r, r.Stored())
		}
		if r.Removable() != removable[r] {
			t.Fatalf("%s.Removable = %v: a hard bounce is permanent and a complaint is the person's choice", r, r.Removable())
		}
	}
}
