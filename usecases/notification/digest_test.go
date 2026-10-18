package notification_usecase

import (
	"context"
	"reflect"
	"strings"
	"testing"
	"time"

	"vozko/domain/notification"
)

type digestRig struct {
	*centerRig
	digests *Digests
}

func newDigestRig(t *testing.T) *digestRig {
	t.Helper()
	rig := newCenterRig(t, teamDirectory(), nil)
	for _, person := range []string{"owner", "agent", "seller"} {
		rig.store.prefs["ws-1/"+person] = notification.Preferences{EmailDigest: true}
	}
	digests, err := NewDigests(DigestsDeps{
		Store: rig.store, Directory: teamDirectory(), Email: rig.emails,
		DashboardURL: "https://app.vozko.test/", Now: rig.clock.now, Location: time.UTC,
	})
	if err != nil {
		t.Fatal(err)
	}
	return &digestRig{centerRig: rig, digests: digests}
}

func (r *digestRig) deliverEmailed(t *testing.T, title string) notification.Record {
	t.Helper()
	d := campaignDraft()
	d.Email, d.Title = true, title
	rec, err := r.publisher.Publish(context.Background(), d)
	if err != nil {
		t.Fatal(err)
	}
	if err := r.delivery.Deliver(context.Background(), rec.ID); err != nil {
		t.Fatal(err)
	}
	return rec
}

func TestTheDigestSendsEachReaderOneEmailWithTheirUnreadHeldItems(t *testing.T) {
	rig := newDigestRig(t)
	first := rig.deliverEmailed(t, "Campanha A pausada")
	rig.deliverEmailed(t, "Campanha B pausada")
	if len(rig.emails.sent) != 0 {
		t.Fatalf("nothing is emailed before the digest, got %v", rig.emails.emails())
	}
	inbox := NewInboxService(rig.store, rig.store, rig.clock.now)
	_, _ = inbox.MarkRead(context.Background(), notification.Viewer{WorkspaceID: "ws-1", UserID: "seller"}, []string{first.ID})

	rig.clock.at = time.Date(2026, 10, 18, 9, 0, 0, 0, time.UTC)
	if err := rig.digests.Send(context.Background()); err != nil {
		t.Fatalf("send: %v", err)
	}

	if got := rig.emails.emails(); !reflect.DeepEqual(got, []string{"agent@x.com", "owner@x.com", "seller@x.com"}) {
		t.Fatalf("digests = %v", got)
	}
	for _, sent := range rig.emails.sent {
		if sent.Template != DigestEmailTemplate || !strings.Contains(sent.Subject, "Resumo") || !strings.HasSuffix(sent.DedupKey, ":2026-10-18") {
			t.Fatalf("digest email = %+v", sent)
		}
		items := sent.Placeholders["Items"].([]digestLine)
		want := 2
		if sent.Email == "seller@x.com" {
			want = 1
		}
		if len(items) != want || sent.Placeholders["Total"] != want || items[0].ActionURL != "https://app.vozko.test/dashboard/whatsapp-campaigns/c-1" {
			t.Fatalf("%s items = %+v total %v", sent.Email, items, sent.Placeholders["Total"])
		}
	}
	for _, person := range []string{"owner", "agent", "seller"} {
		if rig.store.heldFor(person) != 0 {
			t.Fatalf("%s rows must be released after the digest", person)
		}
	}

	if err := rig.digests.Send(context.Background()); err != nil || len(rig.emails.sent) != 3 {
		t.Fatalf("a second tick sends nothing new: %d", len(rig.emails.sent))
	}
}

func TestTheDigestWaitsForTheNextMorningForItemsHeldAfterTheCutoff(t *testing.T) {
	rig := newDigestRig(t)
	rig.clock.at = time.Date(2026, 10, 18, 10, 0, 0, 0, time.UTC)
	rig.deliverEmailed(t, "Depois das oito")

	if err := rig.digests.Send(context.Background()); err != nil {
		t.Fatal(err)
	}
	if len(rig.emails.sent) != 0 || rig.store.heldFor("agent") != 1 {
		t.Fatal("an item held after this morning's cutoff waits for tomorrow")
	}
}

func TestReadersWithEverythingReadOrWhoLeftGetNoDigestAndAreReleased(t *testing.T) {
	rig := newDigestRig(t)
	rec := rig.deliverEmailed(t, "Já lido")
	inbox := NewInboxService(rig.store, rig.store, rig.clock.now)
	for _, person := range []string{"owner", "agent", "seller"} {
		_, _ = inbox.MarkRead(context.Background(), notification.Viewer{WorkspaceID: "ws-1", UserID: person}, []string{rec.ID})
	}
	rig.clock.at = time.Date(2026, 10, 18, 9, 0, 0, 0, time.UTC)
	if err := rig.digests.Send(context.Background()); err != nil {
		t.Fatal(err)
	}
	if len(rig.emails.sent) != 0 || rig.store.heldFor("agent") != 0 {
		t.Fatalf("nothing unread means no email, and the rows are released: %v", rig.emails.emails())
	}
}

func TestAFailedDigestKeepsThatReaderHeldForTheNextTick(t *testing.T) {
	rig := newDigestRig(t)
	rig.deliverEmailed(t, "Campanha pausada")
	rig.emails.err = errBoom
	rig.clock.at = time.Date(2026, 10, 18, 9, 0, 0, 0, time.UTC)

	if err := rig.digests.Send(context.Background()); err == nil {
		t.Fatal("a failed digest is reported")
	}
	if rig.store.heldFor("agent") != 1 {
		t.Fatal("a reader whose digest failed stays held")
	}
	rig.emails.err = nil
	if err := rig.digests.Send(context.Background()); err != nil || len(rig.emails.sent) != 3 {
		t.Fatalf("the next tick delivers: %v", err)
	}
}

func TestDigestsRefuseMissingDependencies(t *testing.T) {
	if _, err := NewDigests(DigestsDeps{}); err == nil {
		t.Fatal("digests without dependencies must not build")
	}
}
