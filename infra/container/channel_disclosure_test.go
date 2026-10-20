package container

import (
	"slices"
	"testing"
)

func TestAChannelWithADisclosureSettingWithoutAnAdapterThatAnswersItIsReported(t *testing.T) {
	gaps := disclosureGaps(t, "testdata/disclosure/domain", "testdata/disclosure/usecases")
	if len(gaps) != 1 || gaps[0] != "acme" {
		t.Fatalf("gaps = %v, want only acme (polite answers its disclosure, quiet has none)", gaps)
	}
}

func TestEveryChannelWithADisclosureSettingHasAnAdapterThatAnswersIt(t *testing.T) {
	channels := disclosingChannels(t, "../../domain", "../../usecases")
	for _, known := range []string{"facebook", "mercadolivre", "tiktok"} {
		if !slices.Contains(channels, known) {
			t.Fatalf("the scan found %v and missed %s, so it no longer sees disclosure settings", channels, known)
		}
	}
	if gaps := disclosureGaps(t, "../../domain", "../../usecases"); len(gaps) > 0 {
		t.Fatalf("channels %v set an automation disclosure but their channel adapter does not implement conversation.DisclosingAdapter; agent continuations would reply without it", gaps)
	}
}
