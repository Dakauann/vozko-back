package marketing

import (
	"encoding/json"
	"reflect"
	"testing"

	"vozko/domain/advertising"
)

func fullTargeting() advertising.Targeting {
	return advertising.Targeting{
		Locations:               []advertising.GeoLocation{{Kind: advertising.LocationCountry, Key: "BR"}},
		ExcludedLocations:       []advertising.GeoLocation{{Kind: advertising.LocationCity, Key: "2700", RadiusKm: 20}},
		AgeMin:                  21,
		AgeMax:                  45,
		Genders:                 []int{advertising.GenderFemale},
		Languages:               []advertising.TargetRef{{ID: "23", Name: "Português"}},
		Interests:               []advertising.TargetRef{{ID: "600", Name: "Futebol"}},
		Behaviors:               []advertising.TargetRef{{ID: "700", Name: "Viajantes"}},
		CustomAudiences:         []advertising.TargetRef{{ID: "CA1"}},
		ExcludedCustomAudiences: []advertising.TargetRef{{ID: "CA2"}},
	}
}

func manualPlacements() advertising.Placements {
	return advertising.Placements{
		Platforms: []string{advertising.PlatformFacebook, advertising.PlatformInstagram, advertising.PlatformThreads},
		Positions: map[string][]string{
			advertising.PlatformFacebook:  {"feed", "story"},
			advertising.PlatformInstagram: {"stream"},
			advertising.PlatformThreads:   {"threads_stream"},
		},
		Devices: []string{advertising.DeviceMobile},
	}
}

const fullTargetingJSON = `{
	"geo_locations":{"countries":["BR"]},
	"excluded_geo_locations":{"cities":[{"key":"2700","radius":20,"distance_unit":"kilometer"}]},
	"age_min":21,"age_max":45,"genders":[2],"locales":[23],
	"flexible_spec":[{"interests":[{"id":"600","name":"Futebol"}],"behaviors":[{"id":"700","name":"Viajantes"}]}],
	"custom_audiences":[{"id":"CA1"}],"excluded_custom_audiences":[{"id":"CA2"}],
	"targeting_automation":{"advantage_audience":0},
	"publisher_platforms":["facebook","instagram","threads"],
	"facebook_positions":["feed","story"],"instagram_positions":["stream"],"threads_positions":["threads_stream"],
	"device_platforms":["mobile"]}`

func TestTargetingSpecWithManualPlacements(t *testing.T) {
	got, err := targetingParam(fullTargeting(), manualPlacements())
	if err != nil {
		t.Fatal(err)
	}
	jsonEqual(t, "targeting", got, fullTargetingJSON)
}

func TestTargetingSpecAutomaticPlacementsAndAdvantageAudience(t *testing.T) {
	target := advertising.Targeting{Locations: []advertising.GeoLocation{{Kind: advertising.LocationCountry, Key: "BR"}}, AgeMin: 18, AgeMax: 65, AdvantageAudience: true}
	got, err := targetingParam(target, advertising.Placements{Automatic: true})
	if err != nil {
		t.Fatal(err)
	}
	jsonEqual(t, "targeting", got, `{"geo_locations":{"countries":["BR"]},"age_min":18,"targeting_automation":{"advantage_audience":1}}`)
}

func TestTargetingSpecRejectsBadInput(t *testing.T) {
	tests := []struct {
		name       string
		targeting  advertising.Targeting
		placements advertising.Placements
	}{
		{name: "language not numeric", targeting: advertising.Targeting{Languages: []advertising.TargetRef{{ID: "pt_BR"}}}, placements: advertising.Placements{Automatic: true}},
		{name: "unknown location kind", targeting: advertising.Targeting{Locations: []advertising.GeoLocation{{Kind: "zip", Key: "13000"}}}, placements: advertising.Placements{Automatic: true}},
		{name: "unknown platform", targeting: advertising.Targeting{}, placements: advertising.Placements{Platforms: []string{"tiktok"}, Positions: map[string][]string{"tiktok": {"feed"}}}},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if _, err := targetingParam(tt.targeting, tt.placements); err == nil {
				t.Fatal("expected an error")
			}
		})
	}
}

func TestTargetingRoundTrip(t *testing.T) {
	target, placements, err := targetingFrom(json.RawMessage(fullTargetingJSON))
	if err != nil {
		t.Fatal(err)
	}
	want := fullTargeting()
	want.Languages = []advertising.TargetRef{{ID: "23"}}
	if !reflect.DeepEqual(*target, want) {
		t.Fatalf("targeting = %+v", *target)
	}
	if !reflect.DeepEqual(*placements, manualPlacements()) {
		t.Fatalf("placements = %+v", *placements)
	}
}

func TestTargetingFromMetaDefaults(t *testing.T) {
	target, placements, err := targetingFrom(json.RawMessage(`{"geo_locations":{"countries":["BR"],"location_types":["home","recent"],
		"regions":[{"key":"460","name":"São Paulo"}],"cities":[{"key":"9","name":"Campinas","radius":10,"distance_unit":"mile"}]},
		"targeting_automation":{"advantage_audience":1},"brand_safety_content_filter_levels":["FACEBOOK_STANDARD"]}`))
	if err != nil {
		t.Fatal(err)
	}
	want := advertising.Targeting{
		Locations: []advertising.GeoLocation{
			{Kind: advertising.LocationCountry, Key: "BR"},
			{Kind: advertising.LocationRegion, Key: "460", Name: "São Paulo"},
			{Kind: advertising.LocationCity, Key: "9", Name: "Campinas", RadiusKm: 16},
		},
		AgeMin: 18, AgeMax: 65, AdvantageAudience: true,
	}
	if !reflect.DeepEqual(*target, want) || !placements.Automatic {
		t.Fatalf("targeting = %+v placements = %+v", *target, *placements)
	}
}

func TestTargetingFromRejectsWhatItCannotRepresent(t *testing.T) {
	tests := map[string]string{
		"narrowed detailed targeting": `{"geo_locations":{"countries":["BR"]},"flexible_spec":[{"interests":[{"id":"1"}]},{"interests":[{"id":"2"}]}]}`,
		"life events":                 `{"geo_locations":{"countries":["BR"]},"flexible_spec":[{"life_events":[{"id":"1"}]}]}`,
		"zip codes":                   `{"geo_locations":{"zips":[{"key":"BR:13000"}]}}`,
		"unknown distance unit":       `{"geo_locations":{"cities":[{"key":"9","radius":10,"distance_unit":"league"}]}}`,
		"unknown advantage flag":      `{"geo_locations":{"countries":["BR"]},"targeting_automation":{"advantage_audience":2}}`,
		"missing":                     ``,
	}
	for name, raw := range tests {
		t.Run(name, func(t *testing.T) {
			if _, _, err := targetingFrom(json.RawMessage(raw)); err == nil {
				t.Fatal("expected an error")
			}
		})
	}
}
