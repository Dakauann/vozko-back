package unofficial_whatsapp_group

import (
	"errors"
	"strings"
	"testing"
	"time"

	uw "vozko/domain/unofficial_whatsapp"
)

func seat(instanceID string, admin, canSend, announce bool, created time.Time) *uw.Group {
	return &uw.Group{InstanceID: instanceID, JID: "120363@g.us", WeAreAdmin: admin, WeCanSend: canSend, Announce: announce, CreatedAt: created}
}

func TestChoosePrimaryPrefersConnectedThenAdminThenPosterThenOldest(t *testing.T) {
	old := time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC)
	newer := old.Add(time.Hour)

	cases := []struct {
		name      string
		presences []Presence
		want      string
	}{
		{"connected beats admin", []Presence{
			{Seat: seat("offline-admin", true, true, false, old), Connected: false},
			{Seat: seat("online-member", false, true, false, newer), Connected: true},
		}, "online-member"},
		{"admin beats member", []Presence{
			{Seat: seat("member", false, true, false, old), Connected: true},
			{Seat: seat("admin", true, true, false, newer), Connected: true},
		}, "admin"},
		{"poster beats silenced", []Presence{
			{Seat: seat("silenced", false, false, true, old), Connected: true},
			{Seat: seat("poster", false, true, true, newer), Connected: true},
		}, "poster"},
		{"oldest seat wins a tie", []Presence{
			{Seat: seat("b-newer", false, true, false, newer), Connected: true},
			{Seat: seat("a-older", false, true, false, old), Connected: true},
		}, "a-older"},
		{"instance id breaks an exact tie", []Presence{
			{Seat: seat("zeta", false, true, false, old), Connected: true},
			{Seat: seat("alpha", false, true, false, old), Connected: true},
		}, "alpha"},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			got, ok := ChoosePrimary(c.presences)
			if !ok || got != c.want {
				t.Fatalf("ChoosePrimary = %q, %v; want %q", got, ok, c.want)
			}
		})
	}
}

func TestChoosePrimaryWithoutSeatsHasNoPrimary(t *testing.T) {
	if got, ok := ChoosePrimary(nil); ok || got != "" {
		t.Fatalf("ChoosePrimary(nil) = %q, %v; want none", got, ok)
	}
}

func TestPresenceCanPostOnlyWhenConnectedAndAllowed(t *testing.T) {
	now := time.Now()
	cases := []struct {
		name string
		p    Presence
		want bool
	}{
		{"open group", Presence{Seat: seat("i", false, true, false, now), Connected: true}, true},
		{"admins only and we are admin", Presence{Seat: seat("i", true, true, true, now), Connected: true}, true},
		{"admins only and we are not", Presence{Seat: seat("i", false, false, true, now), Connected: true}, false},
		{"disconnected number", Presence{Seat: seat("i", true, true, false, now), Connected: false}, false},
		{"no seat", Presence{Connected: true}, false},
	}
	for _, c := range cases {
		if got := c.p.CanPost(); got != c.want {
			t.Fatalf("%s: CanPost = %v, want %v", c.name, got, c.want)
		}
	}
}

func TestNormalizeCategoryTrimsAndBoundsTheLabel(t *testing.T) {
	got, err := NormalizeCategory("  Ofertas  ")
	if err != nil || got != "Ofertas" {
		t.Fatalf("NormalizeCategory = %q, %v", got, err)
	}
	if _, err := NormalizeCategory(strings.Repeat("a", MaxCategoryRunes+1)); !errors.Is(err, ErrCategoryTooLong) {
		t.Fatalf("long category error = %v, want ErrCategoryTooLong", err)
	}
	if got, err := NormalizeCategory(""); err != nil || got != "" {
		t.Fatalf("empty category = %q, %v; want no category", got, err)
	}
}

func TestGroupOriginValidity(t *testing.T) {
	for _, o := range []Origin{OriginManaged, OriginExternal} {
		if !o.Valid() {
			t.Fatalf("%q should be valid", o)
		}
	}
	if Origin("other").Valid() {
		t.Fatalf("unknown origin accepted")
	}
}

func TestFollowedReflectsTheFollowStamp(t *testing.T) {
	at := time.Now()
	if (Group{}).Followed() {
		t.Fatalf("a group without a follow stamp reads as followed")
	}
	if !(Group{FollowedAt: &at}).Followed() {
		t.Fatalf("a followed group reads as not followed")
	}
}

func TestSeriesNameNumbersTheCopyWithinTheNameLimit(t *testing.T) {
	cases := []struct {
		base string
		seq  int
		want string
	}{
		{"Ofertas VIP Bom Preço", 2, "Ofertas VIP Bom Preço #2"},
		{"Ofertas VIP Bom Preço #1", 3, "Ofertas VIP Bom Preço #3"},
		{"Grupo de ofertas da semana", 12, "Grupo de ofertas da s #12"},
	}
	for _, c := range cases {
		got := SeriesName(c.base, c.seq)
		if got != c.want {
			t.Fatalf("SeriesName(%q, %d) = %q, want %q", c.base, c.seq, got, c.want)
		}
		if err := uw.ValidateGroupName(got); err != nil {
			t.Fatalf("SeriesName produced an invalid name %q: %v", got, err)
		}
	}
}

func TestTheWhatsAppCreationDateIsTheEarliestOneASeatReports(t *testing.T) {
	early := time.Date(2024, 3, 1, 12, 0, 0, 0, time.UTC)
	late := early.Add(time.Hour)
	seats := []*uw.Group{{InstanceID: "n1"}, {InstanceID: "n2", GroupCreatedAt: &late}, {InstanceID: "n3", GroupCreatedAt: &early}}

	if got := WhatsAppCreatedAt(seats); got == nil || !got.Equal(early) {
		t.Fatalf("created = %v, want %v", got, early)
	}
	if got := WhatsAppCreatedAt([]*uw.Group{{InstanceID: "n1"}}); got != nil {
		t.Fatalf("created = %v, want unknown without a reported date", got)
	}
}
