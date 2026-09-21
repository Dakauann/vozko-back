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

func (h *groupHarness) withFreshGate() *groupHarness {
	gate := newProfileGate()
	h.uc.profiles.gate = gate
	h.uc.groups.profiles.gate = gate
	return h
}

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

func TestKnownSubjectCostsNoProviderCall(t *testing.T) {
	h := newGroupHarness(t, false).withFreshGate()

	h.deliverPrivate(t, privateMessage("m1", "oi"))
	first := len(h.messaging.chatDetailCalls())

	h.deliverPrivate(t, privateMessage("m2", "ainda aí?"))
	h.deliverPrivate(t, privateMessage("m3", "obrigada"))

	if got := len(h.messaging.chatDetailCalls()); got != first {
		t.Errorf("profile read %d times across three messages, want %d — "+
			"a fresh subject must cost zero calls", got, first)
	}
}

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
	if got := len(h.history.all()); got != 3 {
		t.Errorf("persisted %d backfilled messages, want 3", got)
	}
}

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
	return c.writes[len(c.writes)-1]
}

func (c *capturingContacts) calls() int { return len(c.writes) }

func TestUnchangedPictureIsNotRedownloaded(t *testing.T) {
	h := newGroupHarness(t, false).withFreshGate()
	stored := &capturingContacts{fakeContactRepo: h.contacts}
	h.uc.profiles.contacts = stored

	h.deliverPrivate(t, privateMessage("m1", "oi"))
	if got := len(h.assets.fetched()); got != 1 {
		t.Fatalf("first message downloaded %d avatars, want 1", got)
	}

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

func TestEventWithNoIdentityCreatesNothing(t *testing.T) {
	h := newGroupHarness(t, false).withFreshGate()

	body, _ := json.Marshal(map[string]any{
		"event": "messages", "instance": "prov-1",
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

func TestProfileBurstIsBounded(t *testing.T) {
	h := newGroupHarness(t, false).withFreshGate()
	h.uc.profiles.contacts = &capturingContacts{fakeContactRepo: h.contacts}

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

func TestGatedSubjectIsRetriedLater(t *testing.T) {
	h := newGroupHarness(t, false).withFreshGate()
	stored := &capturingContacts{fakeContactRepo: h.contacts}
	h.uc.profiles.contacts = stored

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

func TestForcedRefreshIgnoresTheGate(t *testing.T) {
	h := newGroupHarness(t, false).withFreshGate()
	stored := &capturingContacts{fakeContactRepo: h.contacts}
	h.uc.profiles.contacts = stored

	subject := &uw.Contact{ID: "c1", InstanceID: "inst-1", JID: "5511999999999@s.whatsapp.net",
		PhoneNumber: "5511999999999"}
	h.contacts.contacts["c1"] = subject

	for i := 0; i < profileReadBurst+5; i++ {
		h.uc.profiles.gate.allow("inst-1")
	}

	h.uc.profiles.refresh(context.Background(), h.instance, subject, true)

	if len(h.messaging.chatDetailCalls()) == 0 {
		t.Error("a forced refresh was blocked by the burst gate; an operator's click must not be")
	}
}
