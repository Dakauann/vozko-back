package marketing

import (
	"encoding/json"
	"fmt"
	"math"
	"slices"
	"sort"
	"strconv"

	"vozko/domain/advertising"
	"vozko/infra/meta"
)

const (
	kilometer        = "kilometer"
	mile             = "mile"
	kilometersInMile = 1.609344
)

type graphPlace struct {
	Key          meta.GraphID `json:"key"`
	Name         string       `json:"name,omitempty"`
	Radius       int          `json:"radius,omitempty"`
	DistanceUnit string       `json:"distance_unit,omitempty"`
}

type graphGeoLocations struct {
	Countries     []string     `json:"countries,omitempty"`
	Regions       []graphPlace `json:"regions,omitempty"`
	Cities        []graphPlace `json:"cities,omitempty"`
	LocationTypes []string     `json:"location_types,omitempty"`
}

type graphFlexibleSpec struct {
	Interests []graphRef `json:"interests,omitempty"`
	Behaviors []graphRef `json:"behaviors,omitempty"`
}

type graphTargetingAutomation struct {
	AdvantageAudience int `json:"advantage_audience"`
}

type graphTargeting struct {
	GeoLocations             *graphGeoLocations        `json:"geo_locations,omitempty"`
	ExcludedGeoLocations     *graphGeoLocations        `json:"excluded_geo_locations,omitempty"`
	AgeMin                   int                       `json:"age_min,omitempty"`
	AgeMax                   int                       `json:"age_max,omitempty"`
	Genders                  []int                     `json:"genders,omitempty"`
	Locales                  []int64                   `json:"locales,omitempty"`
	FlexibleSpec             []graphFlexibleSpec       `json:"flexible_spec,omitempty"`
	CustomAudiences          []graphRef                `json:"custom_audiences,omitempty"`
	ExcludedCustomAudiences  []graphRef                `json:"excluded_custom_audiences,omitempty"`
	TargetingAutomation      *graphTargetingAutomation `json:"targeting_automation,omitempty"`
	PublisherPlatforms       []string                  `json:"publisher_platforms,omitempty"`
	FacebookPositions        []string                  `json:"facebook_positions,omitempty"`
	InstagramPositions       []string                  `json:"instagram_positions,omitempty"`
	MessengerPositions       []string                  `json:"messenger_positions,omitempty"`
	AudienceNetworkPositions []string                  `json:"audience_network_positions,omitempty"`
	ThreadsPositions         []string                  `json:"threads_positions,omitempty"`
	DevicePlatforms          []string                  `json:"device_platforms,omitempty"`
}

var audienceKeys = []string{
	"geo_locations", "excluded_geo_locations", "age_min", "age_max", "genders", "locales",
	"flexible_spec", "custom_audiences", "excluded_custom_audiences", "targeting_automation",
}

var placementKeys = []string{
	"publisher_platforms", "facebook_positions", "instagram_positions", "messenger_positions",
	"audience_network_positions", "threads_positions", "device_platforms",
}

func (t *graphTargeting) positions(platform string) (*[]string, error) {
	switch platform {
	case advertising.PlatformFacebook:
		return &t.FacebookPositions, nil
	case advertising.PlatformInstagram:
		return &t.InstagramPositions, nil
	case advertising.PlatformMessenger:
		return &t.MessengerPositions, nil
	case advertising.PlatformAudienceNetwork:
		return &t.AudienceNetworkPositions, nil
	case advertising.PlatformThreads:
		return &t.ThreadsPositions, nil
	}
	return nil, fmt.Errorf("marketing: unknown placement platform %q", platform)
}

func geoLocationsOf(locations []advertising.GeoLocation) (*graphGeoLocations, error) {
	out := &graphGeoLocations{}
	for _, loc := range locations {
		switch loc.Kind {
		case advertising.LocationCountry:
			out.Countries = append(out.Countries, loc.Key)
		case advertising.LocationRegion:
			out.Regions = append(out.Regions, graphPlace{Key: meta.GraphID(loc.Key)})
		case advertising.LocationCity:
			city := graphPlace{Key: meta.GraphID(loc.Key), Radius: loc.RadiusKm}
			if loc.RadiusKm > 0 {
				city.DistanceUnit = kilometer
			}
			out.Cities = append(out.Cities, city)
		default:
			return nil, fmt.Errorf("marketing: unknown location kind %q", loc.Kind)
		}
	}
	return out, nil
}

func audienceOf(t advertising.Targeting) (graphTargeting, error) {
	geo, err := geoLocationsOf(t.Locations)
	if err != nil {
		return graphTargeting{}, err
	}
	out := graphTargeting{
		GeoLocations:            geo,
		AgeMin:                  t.AgeMin,
		Genders:                 t.Genders,
		CustomAudiences:         refsOf(t.CustomAudiences),
		ExcludedCustomAudiences: refsOf(t.ExcludedCustomAudiences),
		TargetingAutomation:     &graphTargetingAutomation{},
	}
	if t.AdvantageAudience {
		out.TargetingAutomation.AdvantageAudience = 1
	} else {
		out.AgeMax = t.AgeMax
	}
	if len(t.ExcludedLocations) > 0 {
		if out.ExcludedGeoLocations, err = geoLocationsOf(t.ExcludedLocations); err != nil {
			return graphTargeting{}, err
		}
	}
	for _, language := range t.Languages {
		locale, err := strconv.ParseInt(language.ID, 10, 64)
		if err != nil {
			return graphTargeting{}, fmt.Errorf("marketing: language %q is not a meta locale id", language.ID)
		}
		out.Locales = append(out.Locales, locale)
	}
	if len(t.Interests) > 0 || len(t.Behaviors) > 0 {
		out.FlexibleSpec = []graphFlexibleSpec{{Interests: refsOf(t.Interests), Behaviors: refsOf(t.Behaviors)}}
	}
	return out, nil
}

func (t *graphTargeting) setPlacements(p advertising.Placements) error {
	if p.Automatic {
		return nil
	}
	t.PublisherPlatforms = p.Platforms
	for platform, positions := range p.Positions {
		field, err := t.positions(platform)
		if err != nil {
			return err
		}
		*field = positions
	}
	t.DevicePlatforms = p.Devices
	return nil
}

func targetingOf(t advertising.Targeting, p advertising.Placements) (graphTargeting, error) {
	out, err := audienceOf(t)
	if err != nil {
		return graphTargeting{}, err
	}
	if err := out.setPlacements(p); err != nil {
		return graphTargeting{}, err
	}
	return out, nil
}

func targetingParam(t advertising.Targeting, p advertising.Placements) (string, error) {
	spec, err := targetingOf(t, p)
	if err != nil {
		return "", err
	}
	return jsonValue(spec)
}

func rawFields(v any) (map[string]json.RawMessage, error) {
	encoded, err := json.Marshal(v)
	if err != nil {
		return nil, err
	}
	var out map[string]json.RawMessage
	if err := json.Unmarshal(encoded, &out); err != nil {
		return nil, err
	}
	return out, nil
}

func mergeTargeting(current map[string]json.RawMessage, t *advertising.Targeting, p *advertising.Placements) (string, error) {
	merged := make(map[string]json.RawMessage, len(current))
	for k, v := range current {
		merged[k] = v
	}
	replace := func(keys []string, spec graphTargeting) error {
		fields, err := rawFields(spec)
		if err != nil {
			return err
		}
		for _, k := range keys {
			delete(merged, k)
			if v, ok := fields[k]; ok {
				merged[k] = v
			}
		}
		return nil
	}
	if t != nil {
		spec, err := audienceOf(*t)
		if err != nil {
			return "", err
		}
		if err := replace(audienceKeys, spec); err != nil {
			return "", err
		}
	}
	if p != nil {
		var spec graphTargeting
		if err := spec.setPlacements(*p); err != nil {
			return "", err
		}
		if err := replace(placementKeys, spec); err != nil {
			return "", err
		}
	}
	return jsonValue(merged)
}

var knownGeoKeys = []string{"countries", "regions", "cities", "location_types"}

func checkKeys(raw json.RawMessage, allowed []string, what string) error {
	if len(raw) == 0 {
		return nil
	}
	var fields map[string]json.RawMessage
	if err := json.Unmarshal(raw, &fields); err != nil {
		return fmt.Errorf("marketing: unreadable %s: %w", what, err)
	}
	for k := range fields {
		if !slices.Contains(allowed, k) {
			return fmt.Errorf("marketing: %s uses %q, which this editor cannot represent", what, k)
		}
	}
	return nil
}

func locationsOf(geo *graphGeoLocations) ([]advertising.GeoLocation, error) {
	if geo == nil {
		return nil, nil
	}
	var out []advertising.GeoLocation
	for _, c := range geo.Countries {
		out = append(out, advertising.GeoLocation{Kind: advertising.LocationCountry, Key: c})
	}
	for _, r := range geo.Regions {
		out = append(out, advertising.GeoLocation{Kind: advertising.LocationRegion, Key: r.Key.String(), Name: r.Name})
	}
	for _, c := range geo.Cities {
		loc := advertising.GeoLocation{Kind: advertising.LocationCity, Key: c.Key.String(), Name: c.Name}
		switch c.DistanceUnit {
		case "", kilometer:
			loc.RadiusKm = c.Radius
		case mile:
			loc.RadiusKm = int(math.Round(float64(c.Radius) * kilometersInMile))
		default:
			return nil, fmt.Errorf("marketing: city %s has unknown distance unit %q", c.Key, c.DistanceUnit)
		}
		out = append(out, loc)
	}
	return out, nil
}

func targetingFrom(raw json.RawMessage) (*advertising.Targeting, *advertising.Placements, error) {
	if len(raw) == 0 || string(raw) == "null" {
		return nil, nil, fmt.Errorf("marketing: ad set without targeting")
	}
	var fields map[string]json.RawMessage
	if err := json.Unmarshal(raw, &fields); err != nil {
		return nil, nil, fmt.Errorf("marketing: unreadable targeting: %w", err)
	}
	if err := checkKeys(fields["geo_locations"], knownGeoKeys, "geo_locations"); err != nil {
		return nil, nil, err
	}
	if err := checkKeys(fields["excluded_geo_locations"], knownGeoKeys, "excluded_geo_locations"); err != nil {
		return nil, nil, err
	}
	var spec graphTargeting
	if err := json.Unmarshal(raw, &spec); err != nil {
		return nil, nil, fmt.Errorf("marketing: unreadable targeting: %w", err)
	}
	var rawFlexible []json.RawMessage
	if len(fields["flexible_spec"]) > 0 {
		if err := json.Unmarshal(fields["flexible_spec"], &rawFlexible); err != nil {
			return nil, nil, fmt.Errorf("marketing: unreadable flexible_spec: %w", err)
		}
	}
	if len(rawFlexible) > 1 {
		return nil, nil, fmt.Errorf("marketing: flexible_spec narrows with %d groups, which this editor cannot represent", len(rawFlexible))
	}
	for _, group := range rawFlexible {
		if err := checkKeys(group, []string{"interests", "behaviors"}, "flexible_spec"); err != nil {
			return nil, nil, err
		}
	}
	t := &advertising.Targeting{
		AgeMin:                  spec.AgeMin,
		AgeMax:                  spec.AgeMax,
		Genders:                 spec.Genders,
		CustomAudiences:         targetRefs(spec.CustomAudiences),
		ExcludedCustomAudiences: targetRefs(spec.ExcludedCustomAudiences),
	}
	var err error
	if t.Locations, err = locationsOf(spec.GeoLocations); err != nil {
		return nil, nil, err
	}
	if t.ExcludedLocations, err = locationsOf(spec.ExcludedGeoLocations); err != nil {
		return nil, nil, err
	}
	for _, locale := range spec.Locales {
		t.Languages = append(t.Languages, advertising.TargetRef{ID: strconv.FormatInt(locale, 10)})
	}
	if len(spec.FlexibleSpec) == 1 {
		t.Interests = targetRefs(spec.FlexibleSpec[0].Interests)
		t.Behaviors = targetRefs(spec.FlexibleSpec[0].Behaviors)
	}
	if spec.TargetingAutomation != nil {
		switch spec.TargetingAutomation.AdvantageAudience {
		case 0:
		case 1:
			t.AdvantageAudience = true
		default:
			return nil, nil, fmt.Errorf("marketing: unknown advantage_audience %d", spec.TargetingAutomation.AdvantageAudience)
		}
	}
	t.Normalize()
	placements, err := placementsFrom(spec, fields)
	if err != nil {
		return nil, nil, err
	}
	return t, placements, nil
}

func placementsFrom(spec graphTargeting, fields map[string]json.RawMessage) (*advertising.Placements, error) {
	manual := slices.ContainsFunc(placementKeys, func(k string) bool { _, ok := fields[k]; return ok })
	if !manual {
		return &advertising.Placements{Automatic: true}, nil
	}
	out := &advertising.Placements{Platforms: spec.PublisherPlatforms, Devices: spec.DevicePlatforms}
	platforms := make([]string, 0, len(advertising.PlatformPositions()))
	for platform := range advertising.PlatformPositions() {
		platforms = append(platforms, platform)
	}
	sort.Strings(platforms)
	for _, platform := range platforms {
		field, err := spec.positions(platform)
		if err != nil {
			return nil, err
		}
		if len(*field) == 0 {
			continue
		}
		if out.Positions == nil {
			out.Positions = map[string][]string{}
		}
		out.Positions[platform] = *field
	}
	return out, nil
}
