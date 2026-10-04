package advertising

import "testing"

func TestEveryAdAccountWebhookFieldHasAMeaning(t *testing.T) {
	cases := map[string]AccountEventKind{
		"in_process_ad_objects":                       AccountObjectsChanged,
		"with_issues_ad_objects":                      AccountObjectsChanged,
		"field_changed":                               AccountObjectsChanged,
		"effective_status":                            AccountObjectsChanged,
		"creative_fatigue":                            AccountCreativeFatigue,
		"ad_recommendations":                          AccountRecommendation,
		"product_set_issue":                           AccountProductSetIssue,
		"ads_async_creation_request":                  AccountEventUnused,
		"marketing_messages_subscriber_upload_status": AccountEventUnused,
		"something_new":                               AccountEventUnused,
	}
	for field, want := range cases {
		if got := (AdAccountChange{Field: field}).Kind(); got != want {
			t.Fatalf("%s: %s, want %s", field, got, want)
		}
	}
}

func TestObjectReferencesAcceptBothSpellingsOfEachLevel(t *testing.T) {
	cases := map[string]Level{"CAMPAIGN": LevelCampaign, "campaign": LevelCampaign, "AD_SET": LevelAdSet, "adset": LevelAdSet, "AD": LevelAd, "ad": LevelAd}
	for raw, want := range cases {
		ref, ok := ObjectRefOf(" 777 ", raw)
		if !ok || ref != (ObjectRef{MetaID: "777", Level: want}) {
			t.Fatalf("%s: %+v %v", raw, ref, ok)
		}
	}
	for _, args := range [][2]string{{"777", "CREATIVE"}, {"", "AD"}, {"777", ""}} {
		if _, ok := ObjectRefOf(args[0], args[1]); ok {
			t.Fatalf("%v resolved", args)
		}
	}
}

func TestAnObjectChangeThatNamesNoUsableObjectNeedsTheWholeStructure(t *testing.T) {
	if !(AdAccountChange{Field: "with_issues_ad_objects"}).NeedsStructure() {
		t.Fatal("a change without objects skipped the refresh")
	}
	if !(AdAccountChange{Field: "in_process_ad_objects", Unresolved: true}).NeedsStructure() {
		t.Fatal("a creative change skipped the refresh")
	}
	if (AdAccountChange{Field: "field_changed", Objects: []ObjectRef{{MetaID: "1", Level: LevelAd}}}).NeedsStructure() {
		t.Fatal("a precise change asked for the whole structure")
	}
	if (AdAccountChange{Field: "creative_fatigue"}).NeedsStructure() {
		t.Fatal("a field Vozko does not show asked for the whole structure")
	}
}
