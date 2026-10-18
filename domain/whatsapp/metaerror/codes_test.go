package metaerror

import "testing"

func TestCodesAboutTheContactAreNotTemplateFailures(t *testing.T) {
	for _, code := range []int{MarketingOptOut, ExperimentHoldout} {
		if !AboutContact(code) {
			t.Fatalf("%d is about the contact", code)
		}
	}
	for _, code := range []int{SpamLimit, EngagementLimit, Undeliverable, TemplatePaused} {
		if AboutContact(code) {
			t.Fatalf("%d is not about the contact", code)
		}
	}
}

func TestOnlyMetaCodesComeFromMeta(t *testing.T) {
	for _, code := range []int{AccessRemoved, Undeliverable, SpamLimit, TemplateDisabled, 368, 80007} {
		if !FromMeta(code) {
			t.Fatalf("%d comes from Meta", code)
		}
	}
	for _, code := range []int{0, -1, 900008, 900009, 910004, 920002} {
		if FromMeta(code) {
			t.Fatalf("%d is Vozko's own code, not Meta's", code)
		}
	}
}
