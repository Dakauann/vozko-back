package smartcampaign

import (
	"errors"
	"testing"

	businessphone "vozko/domain/whatsapp/business_phone"
)

func TestThePhoneColourDecidesByThePreset(t *testing.T) {
	cases := []struct {
		name    string
		preset  Preset
		quality businessphone.QualityRating
		moment  Moment
		action  Action
		rule    Rule
	}{
		{name: "a green phone keeps sending", preset: Standard, quality: businessphone.QualityRatingGreen, moment: BeforeNextWave, action: Continue},
		{name: "an unrated phone keeps sending", preset: Standard, quality: businessphone.QualityRatingUnknown, moment: BeforeNextWave, action: Continue},
		{name: "standard slows down on a yellow phone", preset: Standard, quality: businessphone.QualityRatingYellow, moment: BeforeNextWave, action: SlowDown, rule: RulePhoneYellow},
		{name: "standard pauses on a red phone in the middle of a wave", preset: Standard, quality: businessphone.QualityRatingRed, moment: DuringWave, action: Pause, rule: RulePhoneRed},
		{name: "aggressive pauses on a red phone", preset: Aggressive, quality: businessphone.QualityRatingRed, moment: BeforeNextWave, action: Pause, rule: RulePhoneRed},
		{name: "conservative pauses on a yellow phone", preset: Conservative, quality: businessphone.QualityRatingYellow, moment: DuringWave, action: Pause, rule: RulePhoneYellow},
		{name: "conservative stops on a red phone", preset: Conservative, quality: businessphone.QualityRatingRed, moment: DuringWave, action: Stop, rule: RulePhoneRed},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			s := healthySnapshot()
			s.Phone.Quality = tc.quality

			d := Decide(PresetPolicy(tc.preset, businessHours()), s, History{}, tc.moment)

			if d.Action != tc.action {
				t.Fatalf("action = %s, want %s (%+v)", d.Action, tc.action, d.Findings)
			}
			if tc.rule != "" && !hasRule(d, tc.rule) {
				t.Fatalf("findings %+v miss %s", d.Findings, tc.rule)
			}
		})
	}
}

func TestAPolicyStoredBeforeThePhoneRulesGetsThePresetDefaults(t *testing.T) {
	stored := PresetPolicy(Aggressive, businessHours())
	stored.OnPhoneYellow, stored.OnPhoneRed = "", ""

	completed := stored.WithPhoneDefaults()

	if completed.OnPhoneYellow != SlowDown || completed.OnPhoneRed != Pause || completed.Validate() != nil {
		t.Fatalf("completed = %s/%s, validate %v", completed.OnPhoneYellow, completed.OnPhoneRed, completed.Validate())
	}
	chosen := PresetPolicy(Aggressive, businessHours())
	chosen.OnPhoneRed = Stop
	if chosen.WithPhoneDefaults().OnPhoneRed != Stop {
		t.Fatal("a chosen action is kept")
	}
}

func TestARedPhoneCannotBeSetToKeepSending(t *testing.T) {
	p := standardPolicy()
	p.OnPhoneRed = Continue
	if err := p.Validate(); !errors.Is(err, ErrInvalidPolicy) {
		t.Fatalf("err = %v", err)
	}
}
