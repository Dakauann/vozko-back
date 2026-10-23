package campaignaudience

import (
	"errors"
	"reflect"
	"testing"
	"time"

	"vozko/domain/campaign"
	"vozko/domain/customfield"
)

var listNow = time.Date(2026, 10, 21, 12, 0, 0, 0, time.UTC)

func TestAListIsReadOnlyByItsUploaderOnItsChannelUntilItExpires(t *testing.T) {
	l := NewList("list-1", "ws-1", "user-1", ChannelWhatsApp, " lista.csv ", campaign.ContactList{Lines: 2}, listNow)
	if l.FileName != "lista.csv" || l.ExpiresAt == nil || !l.ExpiresAt.Equal(listNow.Add(ListTTL)) || l.IssueCounts == nil {
		t.Fatalf("list = %+v", l)
	}
	cases := []struct {
		name      string
		workspace string
		actor     string
		channel   Channel
		at        time.Time
		want      error
	}{
		{name: "the uploader", workspace: "ws-1", actor: "user-1", channel: ChannelWhatsApp, at: listNow, want: nil},
		{name: "another workspace", workspace: "ws-2", actor: "user-1", channel: ChannelWhatsApp, at: listNow, want: ErrListNotFound},
		{name: "another member", workspace: "ws-1", actor: "user-2", channel: ChannelWhatsApp, at: listNow, want: ErrListNotFound},
		{name: "another channel", workspace: "ws-1", actor: "user-1", channel: ChannelEmail, at: listNow, want: ErrListChannelMismatch},
		{name: "after the TTL", workspace: "ws-1", actor: "user-1", channel: ChannelWhatsApp, at: listNow.Add(ListTTL), want: ErrListExpired},
	}
	for _, tc := range cases {
		if err := l.ReadableBy(tc.workspace, tc.actor, tc.channel, tc.at); !errors.Is(err, tc.want) || (tc.want == nil && err != nil) {
			t.Fatalf("%s: %v, want %v", tc.name, err, tc.want)
		}
	}
	if err := (List{}).ReadableBy("", "", ChannelWhatsApp, listNow); !errors.Is(err, ErrListNotFound) {
		t.Fatalf("a missing list = %v", err)
	}
}

func TestAListAttachedToADraftNeverExpiresAndAnswersToItsOwner(t *testing.T) {
	l := NewList("list-1", "ws-1", "user-1", ChannelEmail, "lista.csv", campaign.ContactList{}, listNow)
	l.AttachedTo = EmailCampaignOwner("camp-1")
	l.Touch(listNow)
	if l.ExpiresAt != nil || l.Expired(listNow.Add(365*24*time.Hour)) {
		t.Fatalf("attached list = %+v", l)
	}
	if err := l.ReadableBy("ws-1", "user-2", ChannelEmail, listNow); err != nil {
		t.Fatalf("another member editing the draft = %v", err)
	}
	if err := l.OwnedBy("ws-1", EmailCampaignOwner("camp-1"), ChannelEmail); err != nil {
		t.Fatalf("OwnedBy() = %v", err)
	}
	if err := l.OwnedBy("ws-1", EmailCampaignOwner("camp-2"), ChannelEmail); !errors.Is(err, ErrListNotFound) {
		t.Fatalf("another draft = %v", err)
	}
	if err := l.OwnedBy("ws-1", EmailCampaignOwner("camp-1"), ChannelWhatsApp); !errors.Is(err, ErrListChannelMismatch) {
		t.Fatalf("another channel = %v", err)
	}
}

func TestValidateSuggestionsKeepsOneLeadFieldPerTemplateVariable(t *testing.T) {
	defs := []*customfield.Definition{{Key: "serie", ObjectType: customfield.ObjectLead}, {Key: "cpf", ObjectType: customfield.ObjectLead, Sensitive: true}}
	got, err := ValidateSuggestions([]Suggestion{{Slot: 2, Source: " lead.district "}, {Slot: 1, Source: campaign.CustomBinding("serie")}}, 3, defs)
	if err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(got, []Suggestion{{Slot: 1, Source: campaign.CustomBinding("serie")}, {Slot: 2, Source: campaign.BindDistrict}}) {
		t.Fatalf("suggestions = %+v", got)
	}
	cases := []struct {
		in   []Suggestion
		want error
	}{
		{[]Suggestion{{Slot: 4, Source: campaign.BindCity}}, ErrSuggestionSlot},
		{[]Suggestion{{Slot: 0, Source: campaign.BindCity}}, ErrSuggestionSlot},
		{[]Suggestion{{Slot: 1, Source: campaign.BindCity}, {Slot: 1, Source: campaign.BindName}}, ErrSuggestionDuplicate},
		{[]Suggestion{{Slot: 1, Source: campaign.BindLiteral}}, ErrSuggestionLiteral},
		{[]Suggestion{{Slot: 1, Source: campaign.CustomBinding("cpf")}}, campaign.ErrBindingSensitive},
		{[]Suggestion{{Slot: 1, Source: "lead.cpf"}}, campaign.ErrBindingUnknown},
	}
	for _, tc := range cases {
		if _, err := ValidateSuggestions(tc.in, 3, defs); !errors.Is(err, tc.want) {
			t.Fatalf("ValidateSuggestions(%+v) = %v, want %v", tc.in, err, tc.want)
		}
	}
}

func TestSuggestedSlotsNumberEveryTemplateVariable(t *testing.T) {
	got := SuggestedSlots(3, []Suggestion{{Slot: 2, Source: campaign.BindCity}})
	want := []Slot{{Key: "1"}, {Key: "2", Suggested: campaign.BindCity}, {Key: "3"}}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("SuggestedSlots() = %+v", got)
	}
}

func TestEveryAudienceErrorHasAStableCode(t *testing.T) {
	for _, known := range errorCodes {
		if ErrorCode(known.err) != known.code {
			t.Fatalf("%v maps to %q", known.err, ErrorCode(known.err))
		}
	}
	if ErrorCode(errors.New("other")) != "" || ErrorCode(nil) != "" {
		t.Fatal("unknown errors have no code")
	}
	if len(ErrorCodes()) == 0 {
		t.Fatal("ErrorCodes() is empty")
	}
}

func TestChannelsKnowTheirContactAndWhetherAChoiceIsRequired(t *testing.T) {
	if ChannelEmail.Contact() != campaign.ContactEmail || ChannelWhatsApp.Contact() != campaign.ContactPhone || ChannelCarrier.Contact() != campaign.ContactPhone {
		t.Fatal("contact kinds")
	}
	if !ChannelWhatsApp.RequiresChoice() || !ChannelUnofficial.RequiresChoice() || ChannelEmail.RequiresChoice() || ChannelCarrier.RequiresChoice() {
		t.Fatal("choices")
	}
	if Channel("sms").Valid() || !ChannelCarrier.Valid() {
		t.Fatal("validity")
	}
}
