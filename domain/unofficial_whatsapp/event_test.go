package unofficial_whatsapp

import (
	"encoding/json"
	"strings"
	"testing"
	"time"
)

// The normalizer is where this channel is most likely to be wrong, because the
// provider does not document its webhook payloads at all. Every test here pins a
// classification whose failure mode is silent: a message attributed to the wrong
// person, an AI answering itself, or a transcript that quietly loses a turn.

func envelopeJSON(t *testing.T, event string, data any) *Envelope {
	t.Helper()
	encoded, err := json.Marshal(map[string]any{
		"event": event, "instance": "r18", "data": data,
	})
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}
	env, err := DecodeEnvelope(encoded)
	if err != nil {
		t.Fatalf("DecodeEnvelope: %v", err)
	}
	return env
}

func onlyEvent(t *testing.T, events []*Event) *Event {
	t.Helper()
	if len(events) != 1 {
		t.Fatalf("expected exactly one event, got %d", len(events))
	}
	return events[0]
}

// The three-way split on fromMe is the whole reason this channel can be honest
// about who said what. Each branch has a distinct, expensive failure.
func TestMessageClassification(t *testing.T) {
	cases := []struct {
		name string
		data map[string]any
		want EventKind
		why  string
	}{
		{
			name: "contact wrote",
			data: map[string]any{"fromMe": false, "text": "oi", "messageid": "m1"},
			want: EventInboundMessage,
		},
		{
			name: "our own send coming back",
			data: map[string]any{
				"fromMe": true, "wasSentByApi": true, "messageid": "m2",
				"track_source": TrackSource, "track_id": "entry-1",
			},
			want: EventOutboundEcho,
			why:  "an echo must reconcile, not insert, and must never re-enter automation",
		},
		{
			name: "owner typed on their own phone",
			data: map[string]any{"fromMe": true, "messageid": "m3", "text": "já respondi"},
			want: EventOutboundFromDevice,
			why:  "a real message that belongs in the transcript but must not be answered by the AI",
		},
		{
			name: "another integration sharing the instance",
			data: map[string]any{
				"fromMe": true, "wasSentByApi": true, "messageid": "m4",
				"track_source": "someone-else",
			},
			want: EventOutboundFromDevice,
			why:  "not ours to answer, still part of the transcript",
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			ev := onlyEvent(t, NormalizeEnvelope("inst-1", envelopeJSON(t, "messages", tc.data)))
			if ev.Kind != tc.want {
				t.Errorf("kind = %q, want %q (%s)", ev.Kind, tc.want, tc.why)
			}
		})
	}
}

// Only a live inbound message may trigger the AI and workflows. Every other
// answer here is a loop or a burst.
func TestRunsAutomationGating(t *testing.T) {
	cases := []struct {
		name  string
		event Event
		want  bool
	}{
		{"live inbound", Event{Kind: EventInboundMessage}, true},
		{"echo would have the AI answer itself", Event{Kind: EventOutboundEcho}, false},
		{"owner's phone would have it answer a colleague", Event{Kind: EventOutboundFromDevice}, false},
		{"backfill would answer seven days at once", Event{Kind: EventInboundMessage, Backfill: true}, false},
		{"group would answer the wrong audience", Event{Kind: EventInboundMessage, IsGroup: true}, false},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if got := tc.event.RunsAutomation(); got != tc.want {
				t.Errorf("RunsAutomation() = %v, want %v", got, tc.want)
			}
		})
	}
}

// The timestamp is in MILLISECONDS. Every other channel in this codebase carries
// seconds, and a missed division puts every message in the year 57000 and
// silently destroys inbox ordering.
func TestTimestampIsMilliseconds(t *testing.T) {
	const millis int64 = 1754500000000
	ev := onlyEvent(t, NormalizeEnvelope("inst-1", envelopeJSON(t, "messages", map[string]any{
		"fromMe": false, "messageid": "m1", "messageTimestamp": millis,
	})))

	want := time.UnixMilli(millis).UTC()
	if !ev.Timestamp.Equal(want) {
		t.Errorf("timestamp = %v, want %v", ev.Timestamp, want)
	}
	if ev.Timestamp.Year() != want.Year() {
		t.Errorf("year = %d; a seconds/milliseconds mix-up is the classic bug here", ev.Timestamp.Year())
	}
}

// A provider that sends seconds despite documenting milliseconds must not push
// every message a thousand years into the past.
func TestTimestampToleratesSeconds(t *testing.T) {
	ev := onlyEvent(t, NormalizeEnvelope("inst-1", envelopeJSON(t, "messages", map[string]any{
		"fromMe": false, "messageid": "m1", "messageTimestamp": 1754500000,
	})))
	if ev.Timestamp.Year() < 2020 || ev.Timestamp.Year() > 2100 {
		t.Errorf("timestamp %v is not a plausible date", ev.Timestamp)
	}
}

// A missing timestamp falls back to now rather than to the zero time, which
// would sort the message to the start of every inbox forever.
func TestTimestampMissingFallsBackToNow(t *testing.T) {
	ev := onlyEvent(t, NormalizeEnvelope("inst-1", envelopeJSON(t, "messages", map[string]any{
		"fromMe": false, "messageid": "m1",
	})))
	if ev.Timestamp.IsZero() || time.Since(ev.Timestamp) > time.Minute {
		t.Errorf("timestamp = %v, want approximately now", ev.Timestamp)
	}
}

// Identity resolution decides which CONTACT a message belongs to. Getting it
// wrong attaches one person's message to another's conversation.
func TestSenderIdentityResolution(t *testing.T) {
	t.Run("inbound prefers the resolved phone JID over a LID", func(t *testing.T) {
		ev := onlyEvent(t, NormalizeEnvelope("inst-1", envelopeJSON(t, "messages", map[string]any{
			"fromMe": false, "messageid": "m1",
			"chatid":     "189923456789012@lid",
			"sender":     "189923456789012@lid",
			"sender_pn":  "5511999999999@s.whatsapp.net",
			"sender_lid": "189923456789012@lid",
		})))
		if ev.SenderJID != "5511999999999@s.whatsapp.net" {
			t.Errorf("senderJID = %q, want the phone form", ev.SenderJID)
		}
		if ev.SenderLID != "189923456789012@lid" {
			t.Errorf("senderLID = %q, want it preserved for reconciliation", ev.SenderLID)
		}
		if ev.SenderPhone != "5511999999999" {
			t.Errorf("phone = %q", ev.SenderPhone)
		}
	})

	// For an outbound message the "sender" is OUR number, so the chat is what
	// identifies the other party. Using the sender would attribute every
	// outbound message to the business's own number.
	t.Run("outbound identifies the contact by the chat", func(t *testing.T) {
		ev := onlyEvent(t, NormalizeEnvelope("inst-1", envelopeJSON(t, "messages", map[string]any{
			"fromMe": true, "messageid": "m2",
			"chatid": "5511888888888@s.whatsapp.net",
			"sender": "5511777777777@s.whatsapp.net",
		})))
		if ev.SenderJID != "5511888888888@s.whatsapp.net" {
			t.Errorf("senderJID = %q, want the chat, not our own number", ev.SenderJID)
		}
	})

	// A LID's numeric part is an opaque identifier, not a phone number.
	t.Run("a LID-only contact yields no phone", func(t *testing.T) {
		ev := onlyEvent(t, NormalizeEnvelope("inst-1", envelopeJSON(t, "messages", map[string]any{
			"fromMe": false, "messageid": "m1",
			"chatid": "189923456789012@lid", "sender": "189923456789012@lid",
		})))
		if ev.SenderPhone != "" {
			t.Errorf("phone = %q; a LID must never be read as a phone number", ev.SenderPhone)
		}
	})
}

// A history replay is many messages in one delivery, and it must be marked so
// attendance stays off for all of them.
func TestHistoryBatchIsMarkedAsBackfill(t *testing.T) {
	events := NormalizeEnvelope("inst-1", envelopeJSON(t, "history", []map[string]any{
		{"fromMe": false, "messageid": "h1", "text": "um"},
		{"fromMe": false, "messageid": "h2", "text": "dois"},
	}))
	if len(events) != 2 {
		t.Fatalf("expected 2 events from the batch, got %d", len(events))
	}
	for _, ev := range events {
		if !ev.Backfill {
			t.Error("a history message must be marked as backfill")
		}
		if ev.RunsAutomation() {
			t.Error("a backfilled message must not trigger attendance")
		}
	}
}

// A single object and an array must both decode: the vendor has been observed
// sending either, and rejecting one shape would discard a whole delivery.
func TestMessagesAcceptObjectOrArray(t *testing.T) {
	single := NormalizeEnvelope("inst-1", envelopeJSON(t, "messages",
		map[string]any{"fromMe": false, "messageid": "m1"}))
	batch := NormalizeEnvelope("inst-1", envelopeJSON(t, "messages",
		[]map[string]any{{"fromMe": false, "messageid": "m1"}, {"fromMe": false, "messageid": "m2"}}))

	if len(single) != 1 || len(batch) != 2 {
		t.Errorf("object yielded %d events, array yielded %d", len(single), len(batch))
	}
}

// A status update repeats the same message id for every step. The status must be
// part of the dedup key, or only the first one is processed and the delivery
// track never advances past Sent.
func TestStatusUpdatesAreNotDeduplicatedIntoOne(t *testing.T) {
	keys := map[string]struct{}{}
	for _, status := range []string{"Sent", "Delivered", "Read"} {
		ev := onlyEvent(t, NormalizeEnvelope("inst-1", envelopeJSON(t, "messages_update",
			map[string]any{"messageid": "m1", "status": status, "fromMe": true})))
		if ev.Kind != EventMessageStatus {
			t.Errorf("kind = %q, want a status event", ev.Kind)
		}
		keys[ev.IdempotencyKey] = struct{}{}
	}
	if len(keys) != 3 {
		t.Errorf("the three lifecycle steps collapsed into %d dedup key(s)", len(keys))
	}
}

func TestDeliveryStatusNormalization(t *testing.T) {
	cases := map[string]DeliveryStatus{
		"Sent": DeliverySent, "Delivered": DeliveryDelivered, "Read": DeliveryRead,
		"Failed": DeliveryFailed, "Canceled": DeliveryFailed, "Queued": DeliveryQueued,
		"Deleted": DeliveryDeleted, "something-new": DeliveryUnknown,
	}
	for raw, want := range cases {
		if got := normalizeDeliveryStatus(raw); got != want {
			t.Errorf("normalizeDeliveryStatus(%q) = %q, want %q", raw, got, want)
		}
	}
}

// A deletion must be a tombstone, not a status tick, or a message the customer
// removed stays visible in the CRM.
func TestDeletionIsClassifiedSeparately(t *testing.T) {
	ev := onlyEvent(t, NormalizeEnvelope("inst-1", envelopeJSON(t, "messages_update",
		map[string]any{"messageid": "m1", "status": "Deleted"})))
	if ev.Kind != EventMessageDeleted {
		t.Errorf("kind = %q, want a deletion", ev.Kind)
	}
}

func TestEditIsClassifiedSeparately(t *testing.T) {
	ev := onlyEvent(t, NormalizeEnvelope("inst-1", envelopeJSON(t, "messages_update",
		map[string]any{"messageid": "m1", "status": "Sent", "edited": "2026-08-06T10:00:00Z"})))
	if ev.Kind != EventMessageEdited {
		t.Errorf("kind = %q, want an edit", ev.Kind)
	}
}

// A tapped button carries the option ID a workflow branches on. Losing it routes
// every press down the no-match branch, which reads as "the customer typed
// something unexpected" and is very hard to trace.
func TestInteractiveReplyCarriesItsOptionID(t *testing.T) {
	ev := onlyEvent(t, NormalizeEnvelope("inst-1", envelopeJSON(t, "messages", map[string]any{
		"fromMe": false, "messageid": "m1",
		"text": "Suporte Técnico", "buttonOrListid": "suporte",
	})))
	if ev.OptionID != "suporte" {
		t.Errorf("optionID = %q, want the machine-readable id", ev.OptionID)
	}
}

// A newsletter is a publishing surface, not an attendance surface. Creating a
// conversation for one would put a broadcast channel in an operator's queue.
func TestNewsletterMessagesAreDropped(t *testing.T) {
	events := NormalizeEnvelope("inst-1", envelopeJSON(t, "messages", map[string]any{
		"fromMe": false, "messageid": "m1", "chatid": "120363012345678901@newsletter",
	}))
	if len(events) != 0 {
		t.Errorf("a newsletter post must not become a conversation, got %d event(s)", len(events))
	}
}

func TestGroupMessagesAreClassifiedNotDropped(t *testing.T) {
	ev := onlyEvent(t, NormalizeEnvelope("inst-1", envelopeJSON(t, "messages", map[string]any{
		"fromMe": false, "messageid": "m1", "chatid": "120363012345678901@g.us",
	})))
	if !ev.IsGroup {
		t.Error("a group chat must be marked, so it is stored but inert")
	}
	if ev.Kind != EventInboundMessage {
		t.Errorf("kind = %q; a group message is still a message", ev.Kind)
	}
}

// An event kind the vendor adds must be visible, never discarded: this provider
// ships new types without notice, and a silent drop is indistinguishable from a
// working integration.
func TestUnknownEventIsClassifiedAndKeepsItsPayload(t *testing.T) {
	ev := onlyEvent(t, NormalizeEnvelope("inst-1", envelopeJSON(t, "quantum_flux",
		map[string]any{"whatever": true})))
	if ev.Kind != EventUnknown {
		t.Errorf("kind = %q, want unknown", ev.Kind)
	}
	if len(ev.Raw) == 0 {
		t.Error("the raw payload must survive so the shape can be investigated")
	}
	if ev.IdempotencyKey == "" {
		t.Error("even an unknown event needs a dedup key, or a redelivery is processed twice")
	}
}

// High-volume events with no CRM meaning are classified rather than dropped, so
// their volume stays visible in metrics.
func TestIgnoredEvents(t *testing.T) {
	for _, name := range []string{"presence", "newsletter_messages"} {
		ev := onlyEvent(t, NormalizeEnvelope("inst-1", envelopeJSON(t, name, map[string]any{})))
		if ev.Kind != EventIgnored {
			t.Errorf("%s: kind = %q, want ignored", name, ev.Kind)
		}
	}
}

func TestConnectionEventCarriesItsState(t *testing.T) {
	ev := onlyEvent(t, NormalizeEnvelope("inst-1", envelopeJSON(t, "connection",
		map[string]any{"status": "disconnected"})))
	if ev.Kind != EventConnection || ev.SessionState != "disconnected" {
		t.Errorf("event = %+v", ev)
	}
}

func TestBlockToggleCarriesItsState(t *testing.T) {
	ev := onlyEvent(t, NormalizeEnvelope("inst-1", envelopeJSON(t, "blocks",
		map[string]any{"chatid": "5511999999999@s.whatsapp.net", "isBlocked": true})))
	if ev.Kind != EventBlockToggle || !ev.Blocked {
		t.Errorf("event = %+v", ev)
	}
}

// Dedup keys must be scoped by instance: a provider message id is unique per
// account, not globally, and two workspaces would otherwise suppress each
// other's messages.
func TestIdempotencyKeysAreScopedByInstance(t *testing.T) {
	data := map[string]any{"fromMe": false, "messageid": "shared-id"}
	a := onlyEvent(t, NormalizeEnvelope("inst-a", envelopeJSON(t, "messages", data)))
	b := onlyEvent(t, NormalizeEnvelope("inst-b", envelopeJSON(t, "messages", data)))

	if a.IdempotencyKey == b.IdempotencyKey {
		t.Error("two instances must not share a dedup key for the same provider id")
	}
}

func TestDecodeEnvelopeRejectsGarbage(t *testing.T) {
	for _, body := range []string{"", "not json", `{"instance":"r18"}`} {
		if _, err := DecodeEnvelope([]byte(body)); err == nil {
			t.Errorf("DecodeEnvelope(%q) must fail", body)
		}
	}
}

// The provider substitutes {{...}} from ITS lead store, which is not ours. An
// operator typing it literally, or an agent emitting a stray brace, would
// otherwise leak another record's data into a customer's chat.
func TestSanitizeOutboundTextNeutralisesProviderPlaceholders(t *testing.T) {
	out := SanitizeOutboundText("Olá {{name}}, tudo bem?")
	if out == "Olá {{name}}, tudo bem?" {
		t.Fatal("the provider's placeholder syntax was left intact")
	}
	// The visible text must be unchanged: stripping the braces would silently
	// alter what an operator wrote.
	if len([]rune(out)) < len([]rune("Olá {{name}}, tudo bem?")) {
		t.Errorf("sanitising removed visible characters: %q", out)
	}
	if untouched := SanitizeOutboundText("sem placeholder"); untouched != "sem placeholder" {
		t.Errorf("ordinary text was modified: %q", untouched)
	}
}

// ---------------------------------------------------------------- wire shapes

// Every event type the provider is known to emit must survive decoding and come
// out classified. This is the regression test for the failure that shipped: the
// live host spells the event key `EventType`, the decoder only read `event`, and
// so EVERY inbound webhook — messages included — was answered 200 and thrown
// away. A drop is indistinguishable from silence, which is why it went unnoticed
// until someone watched the log.
func TestEveryProviderEventTypeIsDecodedAndClassified(t *testing.T) {
	// needsKey marks the events where a redelivery would DOUBLE-APPLY. It is
	// false for contact/chat refreshes and for ignored ticks on purpose: a
	// profile update is idempotent, and giving it a payload digest would stop a
	// contact renamed back to an earlier value from ever updating again.
	cases := []struct {
		event    string
		data     map[string]any
		want     EventKind
		needsKey bool
	}{
		{"messages", map[string]any{"fromMe": false, "messageid": "m1", "chatid": "5511999999999@s.whatsapp.net"}, EventInboundMessage, true},
		{"messages_update", map[string]any{"messageid": "m1", "status": "READ"}, EventMessageStatus, true},
		{"history", map[string]any{"fromMe": false, "messageid": "m1"}, EventInboundMessage, true},
		{"connection", map[string]any{"status": "connected"}, EventConnection, true},
		{"chats", map[string]any{"chatid": "5511999999999@s.whatsapp.net"}, EventContactUpdate, false},
		{"contacts", map[string]any{"chatid": "5511999999999@s.whatsapp.net"}, EventContactUpdate, false},
		{"blocks", map[string]any{"chatid": "5511999999999@s.whatsapp.net", "isBlocked": true}, EventBlockToggle, true},
		{"call", map[string]any{"chatid": "5511999999999@s.whatsapp.net"}, EventCall, true},
		{"presence", map[string]any{}, EventIgnored, false},
		{"newsletter_messages", map[string]any{}, EventIgnored, false},
	}

	for _, tc := range cases {
		t.Run(tc.event, func(t *testing.T) {
			// Encoded and decoded through the real path rather than built as a
			// struct: the bug was IN the decoder, so a test that skips it proves
			// nothing about what arrives from the host.
			body, err := json.Marshal(map[string]any{
				"EventType": tc.event, "instance": "r18", "data": tc.data,
			})
			if err != nil {
				t.Fatalf("marshal: %v", err)
			}
			env, err := DecodeEnvelope(body)
			if err != nil {
				t.Fatalf("DecodeEnvelope(%s): %v — the event was dropped", tc.event, err)
			}
			ev := onlyEvent(t, NormalizeEnvelope("inst-1", env))
			if ev.Kind != tc.want {
				t.Errorf("kind = %q, want %q", ev.Kind, tc.want)
			}
			if tc.needsKey && ev.IdempotencyKey == "" {
				t.Error("no dedup key: a redelivery would be processed twice")
			}
		})
	}
}

// The same event arrives spelled several ways depending on which surface of the
// host emitted it. Each variant here was cheap to support and would otherwise
// discard live traffic.
func TestDecodeEnvelopeAcceptsProviderShapeVariants(t *testing.T) {
	cases := []struct {
		name string
		body string
	}{
		{"documented event key", `{"event":"messages","instance":"r18","data":{"messageid":"m1"}}`},
		{"live EventType key", `{"EventType":"messages","instance":"r18","data":{"messageid":"m1"}}`},
		{"snake_case key", `{"event_type":"messages","instance":"r18","data":{"messageid":"m1"}}`},
		{"upper-cased value", `{"EventType":"Messages","instance":"r18","data":{"messageid":"m1"}}`},
		{"payload under the singular event name", `{"EventType":"messages","instance":"r18","message":{"messageid":"m1"}}`},
		{"payload flat at the top level", `{"EventType":"messages","instance":"r18","messageid":"m1"}`},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			env, err := DecodeEnvelope([]byte(tc.body))
			if err != nil {
				t.Fatalf("DecodeEnvelope: %v", err)
			}
			if env.Event != "messages" {
				t.Fatalf("event = %q, want normalized to %q", env.Event, "messages")
			}
			ev := onlyEvent(t, NormalizeEnvelope("inst-1", env))
			// The payload has to be REACHED, not merely accepted: a decoder that
			// returns an envelope with empty Data turns every message into a
			// contentless row, which looks like it works.
			if ev.ProviderMessageID != "m1" {
				t.Errorf("provider message id = %q, want %q — the payload was not located",
					ev.ProviderMessageID, "m1")
			}
		})
	}
}

// The instance second factor must keep working across those variants, since it
// is what stops one tenant's URL from injecting events into another's inbox.
func TestDecodeEnvelopeKeepsTheInstanceIdentifier(t *testing.T) {
	for _, body := range []string{
		`{"EventType":"connection","instance":"r18","data":{}}`,
		`{"EventType":"connection","instanceId":"r18","data":{}}`,
		`{"EventType":"connection","instance_id":"r18","data":{}}`,
	} {
		env, err := DecodeEnvelope([]byte(body))
		if err != nil {
			t.Fatalf("DecodeEnvelope(%s): %v", body, err)
		}
		if env.Instance != "r18" {
			t.Errorf("instance = %q, want r18 — the second factor would not match", env.Instance)
		}
	}
}

// A body we cannot read is usually a real customer message. Reporting its keys
// is how a shape change gets noticed; reporting its VALUES would put message
// text and phone numbers in the log sink, which is the one thing this channel
// must never do.
func TestDescribeUnknownBodyReportsKeysNeverValues(t *testing.T) {
	keys := DescribeUnknownBody([]byte(
		`{"weird":"shape","text":"segredo do cliente","phone":"5511999999999"}`))

	joined := strings.Join(keys, ",")
	for _, secret := range []string{"segredo do cliente", "5511999999999"} {
		if strings.Contains(joined, secret) {
			t.Errorf("a value leaked into the log description: %q", joined)
		}
	}
	for _, want := range []string{"phone", "text", "weird"} {
		if !strings.Contains(joined, want) {
			t.Errorf("key %q missing from %q; the shape is not investigable", want, joined)
		}
	}
	if got := DescribeUnknownBody([]byte("not json")); len(got) != 1 {
		t.Errorf("a non-object body must still describe itself, got %v", got)
	}
}

// A tapped button IS a message, even though `text` is empty.
//
// Regression test for a live drop: an interactive reply normalized to empty
// text, persistence rejected it with "message content is required", the
// consumer treated that as retryable, and the customer's answer was retried
// three times and lost. The operator saw their own question and silence.
func TestInteractiveReplyAlwaysCarriesABody(t *testing.T) {
	cases := []struct {
		name string
		data map[string]any
		want string
	}{
		{
			name: "the button's visible label",
			data: map[string]any{
				"fromMe": false, "messageid": "m1",
				"buttonOrListid": "opt_sim", "buttonOrListText": "Sim, quero agendar",
			},
			want: "Sim, quero agendar",
		},
		{
			name: "a selected list row",
			data: map[string]any{
				"fromMe": false, "messageid": "m2",
				"buttonOrListid": "row_2", "selectedDisplayText": "Plano Pro",
			},
			want: "Plano Pro",
		},
		{
			name: "no label at all falls back to the option id",
			data: map[string]any{
				"fromMe": false, "messageid": "m3", "buttonOrListid": "opt_sim",
			},
			want: "opt_sim",
		},
		{
			name: "a poll vote",
			data: map[string]any{
				"fromMe": false, "messageid": "m4", "vote": "Opção A",
			},
			want: "Opção A",
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			ev := onlyEvent(t, NormalizeEnvelope("inst-1", envelopeJSON(t, "messages", tc.data)))
			if ev.Text != tc.want {
				t.Errorf("text = %q, want %q", ev.Text, tc.want)
			}
			// The machine-readable id must survive alongside it: workflows branch
			// on the id, never on the label an operator can rename.
			if ev.OptionID == "" {
				t.Error("option id lost; workflows would have nothing to branch on")
			}
		})
	}
}

// Explicit text always wins over a label, so a button carrying both does not
// have the customer's own words replaced.
func TestInteractiveReplyKeepsExplicitText(t *testing.T) {
	ev := onlyEvent(t, NormalizeEnvelope("inst-1", envelopeJSON(t, "messages", map[string]any{
		"fromMe": false, "messageid": "m1", "text": "o que o cliente digitou",
		"buttonOrListid": "opt_sim", "buttonOrListText": "Sim",
	})))
	if ev.Text != "o que o cliente digitou" {
		t.Errorf("text = %q; the label overwrote real text", ev.Text)
	}
}
