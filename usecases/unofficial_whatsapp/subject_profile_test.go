package unofficial_whatsapp

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"testing"
	"time"

	uw "vozko/domain/unofficial_whatsapp"
)

// The avatar is the thing that was missing, and the call budget is the thing
// that keeps it affordable.
//
// picture_url existed as a column, was projected by the inbox query, was copied
// onto the DTO and was read by the frontend — and nothing ever wrote it, so
// every conversation on this channel rendered initials. What follows pins both
// halves: that it now gets written, and that writing it does not turn every
// inbound message into a call to WhatsApp.

func privateMessage(id, text string) map[string]any {
	return map[string]any{
		"messageid":        id,
		"chatid":           "5511999999999@s.whatsapp.net",
		"sender":           "5511999999999@s.whatsapp.net",
		"sender_pn":        "5511999999999@s.whatsapp.net",
		"senderName":       "Carla",
		"messageType":      "text",
		"text":             text,
		"messageTimestamp": time.Now().UnixMilli(),
	}
}

func (h *groupHarness) deliverPrivate(t *testing.T, msg map[string]any) {
	t.Helper()
	body, err := json.Marshal(map[string]any{
		"event": "messages", "instance": "prov-1", "data": msg,
	})
	if err != nil {
		t.Fatalf("encoding the provider message: %v", err)
	}
	if err := h.uc.Execute(context.Background(), &QueuedEvent{
		InstanceID: "inst-1", Body: body,
	}); err != nil {
		t.Fatalf("ingest failed: %v", err)
	}
}

// Each harness gets its OWN gate.
//
// The production gate is process-wide on purpose — it is a ceiling on one
// number, and every usecase that enriches must share it. In tests that would
// make the suite order-dependent: whichever test ran first would spend the
// budget and the rest would silently skip enrichment and fail for the wrong
// reason.
func (h *groupHarness) withFreshGate() *groupHarness {
	gate := newProfileGate()
	h.uc.profiles.gate = gate
	h.uc.groups.profiles.gate = gate
	return h
}

// The picture is fetched once, re-hosted on OUR storage, and stored as our URL.
//
// Re-hosting rather than linking is the point: WhatsApp's avatar URLs are
// short-lived and unauthenticated, so a stored link rots within hours and, while
// it works, hands the customer's photo to anyone holding it.
func TestAvatarIsFetchedOnceAndRehosted(t *testing.T) {
	h := newGroupHarness(t, false).withFreshGate()
	stored := &capturingContacts{fakeContactRepo: h.contacts}
	h.uc.profiles.contacts = stored

	h.deliverPrivate(t, privateMessage("m1", "oi"))

	if got := len(h.messaging.chatDetailCalls()); got != 1 {
		t.Fatalf("profile read happened %d times on a first message, want 1", got)
	}
	if got := h.assets.fetched(); len(got) != 1 || got[0] != "https://pps.whatsapp.net/avatar.jpg" {
		t.Fatalf("avatar fetched from %v, want the provider's url once", got)
	}

	keys := h.storage.keys()
	if len(keys) != 1 {
		t.Fatalf("stored %d objects, want 1", len(keys))
	}
	if !strings.HasPrefix(keys[0], "contacts/unofficial_whatsapp/") {
		t.Errorf("stored under %q, want the channel's own namespace", keys[0])
	}

	profile := stored.last()
	if !strings.HasPrefix(profile.PictureURL, "https://cdn.test/") {
		t.Errorf("persisted picture url = %q; a provider link would rot within hours",
			profile.PictureURL)
	}
	if profile.FetchedAt.IsZero() {
		t.Error("the staleness clock was not stamped, so the next message refetches")
	}
}

// A second message from the same person costs NOTHING.
//
// This is the whole budget argument: steady-state traffic makes zero profile
// calls, because the answer is a stored column. An inbox that resolved identity
// on read would turn one page into fifty calls on the number the send path is
// using, and a number that looks like it is hammering the API is a number that
// gets banned.
func TestKnownSubjectCostsNoProviderCall(t *testing.T) {
	h := newGroupHarness(t, false).withFreshGate()

	h.deliverPrivate(t, privateMessage("m1", "oi"))
	first := len(h.messaging.chatDetailCalls())

	// The fake repository stores what UpdateProfile wrote, so the second
	// delivery sees a subject whose clock has just been stamped.
	h.deliverPrivate(t, privateMessage("m2", "ainda aí?"))
	h.deliverPrivate(t, privateMessage("m3", "obrigada"))

	if got := len(h.messaging.chatDetailCalls()); got != first {
		t.Errorf("profile read %d times across three messages, want %d — "+
			"a fresh subject must cost zero calls", got, first)
	}
}

// A backfill NEVER enriches.
//
// Connecting an instance replays up to seven days of history in one burst. A
// profile read per replayed message would be hundreds of calls in a few seconds
// on a number that has just come online — the most automated-looking thing this
// channel could do.
func TestBackfillNeverCallsTheProvider(t *testing.T) {
	h := newGroupHarness(t, false).withFreshGate()

	body, _ := json.Marshal(map[string]any{
		"event": "history", "instance": "prov-1",
		"data": []map[string]any{
			privateMessage("h1", "primeira"),
			privateMessage("h2", "segunda"),
			privateMessage("h3", "terceira"),
		},
	})
	if err := h.uc.Execute(context.Background(), &QueuedEvent{InstanceID: "inst-1", Body: body}); err != nil {
		t.Fatalf("history ingest failed: %v", err)
	}

	if got := h.messaging.chatDetailCalls(); len(got) != 0 {
		t.Errorf("a history replay made %d profile calls, want 0", len(got))
	}
	if got := h.assets.fetched(); len(got) != 0 {
		t.Errorf("a history replay downloaded %d avatars, want 0", len(got))
	}
	// The transcript is still complete: the messages are what matter, the
	// pictures are cosmetic and arrive with the first live message.
	if got := len(h.history.all()); got != 3 {
		t.Errorf("persisted %d backfilled messages, want 3", got)
	}
}

// A failed read still burns the clock.
//
// Without this a subject whose picture 404s is retried on the next inbound
// message, and the one after that, forever — turning a missing avatar into a
// permanent per-message call to WhatsApp.
func TestFailedProfileReadStillStampsTheClock(t *testing.T) {
	h := newGroupHarness(t, false).withFreshGate()
	stored := &capturingContacts{fakeContactRepo: h.contacts}
	h.uc.profiles.contacts = stored
	h.messaging.ChatDetailsFn = func(context.Context, uw.InstanceRef, string) (*uw.ChatProfile, error) {
		return nil, errors.New("the host is having a bad day")
	}

	h.deliverPrivate(t, privateMessage("m1", "oi"))

	profile := stored.last()
	if profile.FetchedAt.IsZero() {
		t.Fatal("a failed read left the clock unstamped; every message would retry it")
	}
	if profile.PictureURL != "" {
		t.Error("a failed read must not invent a picture url")
	}
}

// The free name refresh must not consume the profile budget.
//
// These were one function, and its first guard — "the event carried no new
// name" — returned before the staleness clock was ever consulted. That is why
// the provider read never happened and picture_url was empty for every contact
// this channel ever created. They are two decisions and they are now two calls.
func TestEventNameRefreshDoesNotStampTheProfileClock(t *testing.T) {
	h := newGroupHarness(t, false).withFreshGate()
	stored := &capturingContacts{fakeContactRepo: h.contacts}

	subject := &uw.Contact{ID: "c1", InstanceID: "inst-1", JID: "5511999999999@s.whatsapp.net", Name: "Antigo"}
	sync := subjectProfile{contacts: stored, ttl: profileTTL, now: nowUTC}
	sync.applyEventName(context.Background(), subject, "Novo")

	profile := stored.last()
	if profile.Name != "Novo" {
		t.Errorf("name = %q, want the one the event carried", profile.Name)
	}
	if !profile.FetchedAt.IsZero() {
		t.Error("a free name refresh stamped the profile clock, which suppresses the picture fetch")
	}
}

// A group's subject is never renamed after whoever spoke last.
//
// The push name on a group message belongs to the PARTICIPANT. Writing it onto
// the subject would rename "Time Comercial" to "Ana" the moment Ana wrote.
func TestGroupSubjectIsNotRenamedByParticipants(t *testing.T) {
	h := newGroupHarness(t, false).withFreshGate()
	stored := &capturingContacts{fakeContactRepo: h.contacts}

	subject := &uw.Contact{
		ID: "g1", InstanceID: "inst-1",
		JID: "120363012345678901@g.us", IsGroup: true, Name: "Time Comercial",
	}
	sync := subjectProfile{contacts: stored, ttl: profileTTL, now: nowUTC}
	sync.applyEventName(context.Background(), subject, "Ana")

	if stored.calls() != 0 {
		t.Error("a participant's push name was written onto the group's subject")
	}
	if subject.Name != "Time Comercial" {
		t.Errorf("group subject = %q, want it unchanged", subject.Name)
	}
}

// capturingContacts records profile writes and applies them, so a second
// delivery sees the state the first one persisted.
type capturingContacts struct {
	*fakeContactRepo
	writes []uw.ContactProfile
}

func (c *capturingContacts) UpdateProfile(ctx context.Context, id string, p uw.ContactProfile) error {
	c.writes = append(c.writes, p)

	contact, err := c.fakeContactRepo.FindByID(ctx, id)
	if err != nil {
		return nil
	}
	if p.Name != "" {
		contact.Name = p.Name
	}
	if p.ContactName != "" {
		contact.ContactName = p.ContactName
	}
	if p.PictureURL != "" {
		contact.PictureURL = p.PictureURL
	}
	if !p.FetchedAt.IsZero() {
		at := p.FetchedAt
		contact.ProfileFetchedAt = &at
	}
	return nil
}

func (c *capturingContacts) last() uw.ContactProfile {
	if len(c.writes) == 0 {
		return uw.ContactProfile{}
	}
	// The profile read is the last write: applyEventName runs first and is
	// free, refresh runs second and is the one that carries the picture.
	return c.writes[len(c.writes)-1]
}

func (c *capturingContacts) calls() int { return len(c.writes) }

// A picture whose source url has not changed is never re-downloaded.
//
// WhatsApp's avatar urls carry a content id, so an unchanged url is an unchanged
// photo. Without this comparison the weekly refresh re-downloads and re-uploads
// every contact's picture forever; with it, a refresh that finds nothing new
// costs the read and nothing else.
func TestUnchangedPictureIsNotRedownloaded(t *testing.T) {
	h := newGroupHarness(t, false).withFreshGate()
	stored := &capturingContacts{fakeContactRepo: h.contacts}
	h.uc.profiles.contacts = stored

	h.deliverPrivate(t, privateMessage("m1", "oi"))
	if got := len(h.assets.fetched()); got != 1 {
		t.Fatalf("first message downloaded %d avatars, want 1", got)
	}

	// The subject is now stale again, so the profile IS re-read — but the
	// provider hands back the same url.
	subject := h.contacts.contacts["contact-5511999999999@s.whatsapp.net"]
	subject.ProfileFetchedAt = nil
	h.deliverPrivate(t, privateMessage("m2", "de novo"))

	if got := len(h.messaging.chatDetailCalls()); got != 2 {
		t.Errorf("profile read %d times, want 2 — the subject was stale again", got)
	}
	if got := len(h.assets.fetched()); got != 1 {
		t.Errorf("the avatar was downloaded %d times; an unchanged url must skip it", got)
	}
}

// A CHANGED picture is picked up on the next read.
func TestChangedPictureIsRehosted(t *testing.T) {
	h := newGroupHarness(t, false).withFreshGate()
	stored := &capturingContacts{fakeContactRepo: h.contacts}
	h.uc.profiles.contacts = stored

	h.deliverPrivate(t, privateMessage("m1", "oi"))

	h.messaging.ChatDetailsFn = func(_ context.Context, _ uw.InstanceRef, chatID string) (*uw.ChatProfile, error) {
		return &uw.ChatProfile{JID: chatID, PictureURL: "https://pps.whatsapp.net/NEW.jpg"}, nil
	}
	subject := h.contacts.contacts["contact-5511999999999@s.whatsapp.net"]
	subject.ProfileFetchedAt = nil
	h.deliverPrivate(t, privateMessage("m2", "troquei a foto"))

	if got := len(h.assets.fetched()); got != 2 {
		t.Fatalf("the avatar was downloaded %d times; a changed url must be re-hosted", got)
	}
	if got := stored.last().PictureSourceURL; got != "https://pps.whatsapp.net/NEW.jpg" {
		t.Errorf("source url = %q, want the new one — otherwise every read re-downloads", got)
	}
}

// A `contacts`/`chats` event carries the vendor's whole Chat object, picture
// included, so a profile-photo change arrives as a PUSH.
//
// This is the cheapest path in the channel: zero provider calls, one CDN GET,
// and only when the picture actually changed.
func TestPushedProfilePictureCostsNoProviderCall(t *testing.T) {
	h := newGroupHarness(t, false).withFreshGate()
	stored := &capturingContacts{fakeContactRepo: h.contacts}
	h.uc.profiles.contacts = stored

	h.deliverPrivate(t, privateMessage("m1", "oi"))
	callsBefore := len(h.messaging.chatDetailCalls())
	fetchesBefore := len(h.assets.fetched())

	body, _ := json.Marshal(map[string]any{
		"event": "chats", "instance": "prov-1",
		"data": map[string]any{
			"wa_chatid":      "5511999999999@s.whatsapp.net",
			"wa_contactName": "Carla Nova",
			"imagePreview":   "https://pps.whatsapp.net/CHANGED.jpg",
		},
	})
	if err := h.uc.Execute(context.Background(), &QueuedEvent{InstanceID: "inst-1", Body: body}); err != nil {
		t.Fatalf("chats event failed: %v", err)
	}

	if got := len(h.messaging.chatDetailCalls()); got != callsBefore {
		t.Errorf("a pushed profile update made %d extra provider calls, want 0",
			got-callsBefore)
	}
	if got := len(h.assets.fetched()); got != fetchesBefore+1 {
		t.Fatalf("the pushed picture was not re-hosted (%d fetches, want %d)",
			got, fetchesBefore+1)
	}
	if got := stored.last().PictureSourceURL; got != "https://pps.whatsapp.net/CHANGED.jpg" {
		t.Errorf("source url = %q, want the pushed one", got)
	}
}

// The same pushed picture arriving twice costs one string comparison.
func TestRepeatedPushedPictureIsIgnored(t *testing.T) {
	h := newGroupHarness(t, false).withFreshGate()
	h.uc.profiles.contacts = &capturingContacts{fakeContactRepo: h.contacts}
	h.deliverPrivate(t, privateMessage("m1", "oi"))

	body, _ := json.Marshal(map[string]any{
		"event": "chats", "instance": "prov-1",
		"data": map[string]any{
			"wa_chatid":    "5511999999999@s.whatsapp.net",
			"imagePreview": "https://pps.whatsapp.net/SAME.jpg",
		},
	})
	fetchesBefore := len(h.assets.fetched())
	for i := 0; i < 3; i++ {
		if err := h.uc.Execute(context.Background(), &QueuedEvent{InstanceID: "inst-1", Body: body}); err != nil {
			t.Fatalf("chats event failed: %v", err)
		}
	}

	if got := len(h.assets.fetched()) - fetchesBefore; got != 1 {
		t.Errorf("three identical pushes downloaded %d times, want 1", got)
	}
}

// An event that names neither a chat nor a sender must not be filed.
//
// Regression test for a live symptom. Contact identity is uniquely
// (instance, jid), so an event with no identity resolved to a contact with an
// EMPTY jid — and because that row is unique, every later unattributable event
// resolved to the SAME one. The result was one catch-all conversation per
// connected number, sitting in the inbox titled "unofficial_whatsapp" (the
// last-resort label for a contact with no name and no handle) and filling with
// "[mensagem sem conteúdo]".
func TestEventWithNoIdentityCreatesNothing(t *testing.T) {
	h := newGroupHarness(t, false).withFreshGate()

	body, _ := json.Marshal(map[string]any{
		"event": "messages", "instance": "prov-1",
		// The shape a decoder gap produces: a readable envelope wrapping a
		// message whose fields we do not recognise.
		"data": map[string]any{"remoteJid": "5511999999999@s.whatsapp.net", "body": "oi"},
	})
	if err := h.uc.Execute(context.Background(), &QueuedEvent{InstanceID: "inst-1", Body: body}); err != nil {
		t.Fatalf("an unattributable event must not fail the delivery: %v", err)
	}

	if len(h.contacts.created) != 0 {
		t.Errorf("created %d contacts from an event that identifies nobody", len(h.contacts.created))
	}
	if len(h.convs.created) != 0 {
		t.Errorf("created %d conversations from an event that identifies nobody", len(h.convs.created))
	}
	if len(h.history.all()) != 0 {
		t.Error("persisted a message that could not be attributed to any chat")
	}
}

// A profile is read by NUMBER, not by JID.
//
// The provider's chat-details endpoint documents its argument as "a phone number
// or a group id". A subject first seen under a LID has a JID of the form
// "…@lid", which identifies nobody outside WhatsApp's privacy layer — asking
// with it returns nothing, which is indistinguishable from "this person has no
// picture". A group is still addressed by its JID, because that IS its id.
func TestProfileIsReadByNumberNotJID(t *testing.T) {
	h := newGroupHarness(t, false).withFreshGate()
	h.uc.profiles.contacts = &capturingContacts{fakeContactRepo: h.contacts}

	h.deliverPrivate(t, privateMessage("m1", "oi"))

	calls := h.messaging.chatDetailCalls()
	if len(calls) != 1 {
		t.Fatalf("profile read %d times, want 1", len(calls))
	}
	if calls[0] != "5511999999999" {
		t.Errorf("profile read addressed %q, want the bare number", calls[0])
	}
}

func TestGroupProfileIsReadByJID(t *testing.T) {
	group := &uw.Contact{JID: "120363012345678901@g.us", IsGroup: true}
	if got := group.ProfileRef(); got != group.JID {
		t.Errorf("group ProfileRef() = %q, want its jid", got)
	}

	lidOnly := &uw.Contact{JID: "189923456789012@lid"}
	if got := lidOnly.ProfileRef(); got != lidOnly.JID {
		t.Errorf("a LID-only subject has nothing better to offer; got %q", got)
	}
}

// A BURST of new contacts must not become a burst of provider calls.
//
// The staleness clock bounds how often ONE subject is read and says nothing
// about how many are read at once — and the webhook consumer runs twenty message
// workers. Twenty parallel identity lookups against a single WhatsApp account is
// what a scraper looks like, on a channel where looking automated costs the
// customer their number.
//
// The excess is SKIPPED, not queued: the clock is only stamped when a read
// actually happened, so a deferred subject stays stale and the next message
// retries it. The backlog drains itself with no queue to build or lose.
func TestProfileBurstIsBounded(t *testing.T) {
	h := newGroupHarness(t, false).withFreshGate()
	h.uc.profiles.contacts = &capturingContacts{fakeContactRepo: h.contacts}

	// Twenty different people, all unknown, all arriving at once.
	const arrivals = 20
	for i := 0; i < arrivals; i++ {
		number := fmt.Sprintf("55119000000%02d", i)
		body, _ := json.Marshal(map[string]any{
			"event": "messages", "instance": "prov-1",
			"data": map[string]any{
				"messageid": fmt.Sprintf("m%d", i),
				"chatid":    number + "@s.whatsapp.net",
				"sender":    number + "@s.whatsapp.net",
				"sender_pn": number + "@s.whatsapp.net",
				"text":      "oi", "messageTimestamp": time.Now().UnixMilli(),
			},
		})
		if err := h.uc.Execute(context.Background(), &QueuedEvent{InstanceID: "inst-1", Body: body}); err != nil {
			t.Fatalf("ingest %d failed: %v", i, err)
		}
	}

	// Every message still landed. That is the non-negotiable half: enrichment is
	// cosmetic and must never cost a customer's message.
	if got := len(h.history.all()); got != arrivals {
		t.Fatalf("persisted %d messages, want %d — enrichment must never drop one", got, arrivals)
	}

	reads := len(h.messaging.chatDetailCalls())
	if reads > profileReadBurst {
		t.Errorf("a burst of %d new contacts made %d profile reads; the per-instance ceiling is %d",
			arrivals, reads, profileReadBurst)
	}
	if reads == 0 {
		t.Error("the gate closed completely; the first arrivals must still enrich")
	}
}

// A skipped subject stays STALE, so the next message retries it.
//
// If the gate stamped the clock it would silence that subject for a whole TTL
// and the deferral would become a permanent loss.
func TestGatedSubjectIsRetriedLater(t *testing.T) {
	h := newGroupHarness(t, false).withFreshGate()
	stored := &capturingContacts{fakeContactRepo: h.contacts}
	h.uc.profiles.contacts = stored

	// Spend the whole budget on other people.
	for i := 0; i < profileReadBurst; i++ {
		number := fmt.Sprintf("55119100000%02d", i)
		body, _ := json.Marshal(map[string]any{
			"event": "messages", "instance": "prov-1",
			"data": map[string]any{
				"messageid": fmt.Sprintf("b%d", i),
				"chatid":    number + "@s.whatsapp.net",
				"sender":    number + "@s.whatsapp.net",
				"text":      "oi", "messageTimestamp": time.Now().UnixMilli(),
			},
		})
		_ = h.uc.Execute(context.Background(), &QueuedEvent{InstanceID: "inst-1", Body: body})
	}

	h.deliverPrivate(t, privateMessage("m1", "oi"))
	subject := h.contacts.contacts["contact-5511999999999@s.whatsapp.net"]
	if subject == nil {
		t.Fatal("the gated subject was not created; the message path must not depend on enrichment")
	}
	if subject.ProfileFetchedAt != nil {
		t.Error("a deferred enrichment stamped the clock, which would silence this subject for a week")
	}
}

// A FORCED read is exempt.
//
// It comes from an operator's click or from an invalidation the provider itself
// sent — rare, someone is waiting on it, and neither is a burst.
func TestForcedRefreshIgnoresTheGate(t *testing.T) {
	h := newGroupHarness(t, false).withFreshGate()
	stored := &capturingContacts{fakeContactRepo: h.contacts}
	h.uc.profiles.contacts = stored

	subject := &uw.Contact{ID: "c1", InstanceID: "inst-1", JID: "5511999999999@s.whatsapp.net",
		PhoneNumber: "5511999999999"}
	h.contacts.contacts["c1"] = subject

	// Drain the budget entirely.
	for i := 0; i < profileReadBurst+5; i++ {
		h.uc.profiles.gate.allow("inst-1")
	}

	h.uc.profiles.refresh(context.Background(), h.instance, subject, true)

	if len(h.messaging.chatDetailCalls()) == 0 {
		t.Error("a forced refresh was blocked by the burst gate; an operator's click must not be")
	}
}
