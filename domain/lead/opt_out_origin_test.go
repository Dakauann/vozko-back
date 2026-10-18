package lead

import (
	"errors"
	"slices"
	"testing"

	"vozko/domain/crmfilter"
)

func TestOptOutOriginsAreTheSourcesThatWithdrawPlusTheWordKinds(t *testing.T) {
	want := []OptOutOrigin{
		OptOutOrigin(ConsentButtonReply), OptOutTemplateWord, OptOutCampaignWord, OptOutWorkspaceWord, OptOutOrigin(ConsentKeyword),
		OptOutOrigin(ConsentAIAgent), OptOutOrigin(ConsentWorkflow), OptOutOrigin(ConsentManual),
		OptOutOrigin(ConsentConversationRequest), OptOutOrigin(ConsentElo), OptOutOrigin(ConsentAPI),
	}
	if got := OptOutOrigins(); !slices.Equal(got, want) {
		t.Fatalf("origins = %v, want %v", got, want)
	}
	for _, source := range []ConsentSource{ConsentForm, ConsentImport, "", "operator"} {
		if OptOutOrigin(source).Valid() {
			t.Errorf("%q cannot record an opt-out and must not be offered", source)
		}
	}
}

func TestEachOptOutOriginNamesTheSourceItIsStoredAs(t *testing.T) {
	cases := map[OptOutOrigin]ConsentSource{
		OptOutTemplateWord:               ConsentKeyword,
		OptOutCampaignWord:               ConsentKeyword,
		OptOutWorkspaceWord:              ConsentKeyword,
		OptOutOrigin(ConsentKeyword):     ConsentKeyword,
		OptOutOrigin(ConsentButtonReply): ConsentButtonReply,
		OptOutOrigin(ConsentManual):      ConsentManual,
	}
	for origin, want := range cases {
		if got := origin.Source(); got != want {
			t.Errorf("%s: source %s, want %s", origin, got, want)
		}
	}
	if trigger, ok := OptOutTemplateWord.WordTrigger(); !ok || trigger != "template_word" {
		t.Fatalf("template word trigger = %q, %v", trigger, ok)
	}
	if trigger, ok := OptOutCampaignWord.WordTrigger(); !ok || trigger != "campaign_word" {
		t.Fatalf("campaign word trigger = %q, %v", trigger, ok)
	}
	if _, ok := OptOutOrigin(ConsentKeyword).WordTrigger(); ok {
		t.Fatal("keyword covers every word and narrows to no trigger")
	}
}

func TestTheLeadFilterChecksHowTheLeadLeft(t *testing.T) {
	filter := func(values ...string) crmfilter.Filter {
		return crmfilter.Filter{Groups: []crmfilter.Group{{Conjunction: crmfilter.And, Predicates: []crmfilter.Predicate{
			{Field: crmfilter.FieldOptOutSource, Operator: crmfilter.OpIn, Values: values},
		}}}}
	}
	if err := ValidateFilter(filter("button_reply", "campaign_word", "workspace_word", "ai_agent")); err != nil {
		t.Fatalf("known origins: %v", err)
	}
	for _, unknown := range []string{"form", "import", "lead_request", "operator", "BUTTON_REPLY"} {
		if err := ValidateFilter(filter(unknown)); !errors.Is(err, crmfilter.ErrInvalidValue) {
			t.Errorf("%q: got %v, want ErrInvalidValue", unknown, err)
		}
	}
}
