package advertising

import (
	"net/url"
	"slices"
	"strings"
)

type LocationKind string

const (
	LocationCountry LocationKind = "country"
	LocationRegion  LocationKind = "region"
	LocationCity    LocationKind = "city"
)

type GeoLocation struct {
	Kind     LocationKind `json:"kind"`
	Key      string       `json:"key"`
	Name     string       `json:"name"`
	RadiusKm int          `json:"radiusKm,omitempty"`
}

type TargetRef struct {
	ID   string `json:"id"`
	Name string `json:"name"`
}

const (
	GenderMale   = 1
	GenderFemale = 2
)

type Targeting struct {
	Locations               []GeoLocation `json:"locations"`
	ExcludedLocations       []GeoLocation `json:"excludedLocations,omitempty"`
	AgeMin                  int           `json:"ageMin"`
	AgeMax                  int           `json:"ageMax"`
	Genders                 []int         `json:"genders,omitempty"`
	Languages               []TargetRef   `json:"languages,omitempty"`
	Interests               []TargetRef   `json:"interests,omitempty"`
	Behaviors               []TargetRef   `json:"behaviors,omitempty"`
	CustomAudiences         []TargetRef   `json:"customAudiences,omitempty"`
	ExcludedCustomAudiences []TargetRef   `json:"excludedCustomAudiences,omitempty"`
	AdvantageAudience       bool          `json:"advantageAudience"`
}

const (
	PlatformFacebook        = "facebook"
	PlatformInstagram       = "instagram"
	PlatformMessenger       = "messenger"
	PlatformAudienceNetwork = "audience_network"
	PlatformThreads         = "threads"
	DeviceMobile            = "mobile"
	DeviceDesktop           = "desktop"
)

var platformPositions = map[string][]string{
	PlatformFacebook:        {"feed", "right_hand_column", "marketplace", "story", "search", "instream_video", "facebook_reels", "facebook_reels_overlay", "profile_feed", "notification"},
	PlatformInstagram:       {"stream", "story", "explore_home", "reels", "profile_feed", "ig_search", "profile_reels"},
	PlatformMessenger:       {"sponsored_messages"},
	PlatformAudienceNetwork: {"classic", "rewarded_video"},
	PlatformThreads:         {"threads_stream"},
}

var positionsNeedingFeed = []string{"marketplace", "search", "profile_feed", "notification"}

func PlatformPositions() map[string][]string {
	out := make(map[string][]string, len(platformPositions))
	for k, v := range platformPositions {
		out[k] = append([]string(nil), v...)
	}
	return out
}

type Placements struct {
	Automatic bool                `json:"automatic"`
	Platforms []string            `json:"platforms,omitempty"`
	Positions map[string][]string `json:"positions,omitempty"`
	Devices   []string            `json:"devices,omitempty"`
}

func (p Placements) Includes(platform string) bool {
	return p.Automatic || slices.Contains(p.Platforms, platform)
}

const (
	maxLocations       = 50
	maxTargetRefs      = 200
	minAge             = 13
	maxAge             = 65
	restrictedMinAge   = 18
	minCityRadiusKm    = 17
	maxCityRadiusKm    = 80
	restrictedRadiusKm = 25
	advantageMaxAgeMin = 25
)

func (t *Targeting) Normalize() {
	if t.AgeMin == 0 {
		t.AgeMin = restrictedMinAge
	}
	if t.AgeMax == 0 {
		t.AgeMax = maxAge
	}
}

func (t Targeting) validate(v issues, restricted bool) {
	if len(t.Locations) == 0 {
		v.add("locations", "required")
	}
	if len(t.Locations) > maxLocations || len(t.ExcludedLocations) > maxLocations {
		v.add("locations", "too_many")
	}
	for _, loc := range t.Locations {
		checkLocation(v.at("locations"), loc, restricted)
	}
	for _, loc := range t.ExcludedLocations {
		checkLocation(v.at("excludedLocations"), loc, false)
	}
	if t.AgeMin < minAge || t.AgeMax > maxAge || t.AgeMin > t.AgeMax {
		v.add("age", "invalid")
	}
	for _, g := range t.Genders {
		if g != GenderMale && g != GenderFemale {
			v.add("genders", "invalid")
		}
	}
	for field, refs := range map[string][]TargetRef{
		"languages": t.Languages, "interests": t.Interests, "behaviors": t.Behaviors,
		"customAudiences": t.CustomAudiences, "excludedCustomAudiences": t.ExcludedCustomAudiences,
	} {
		checkRefs(v, field, refs)
	}
	for _, ref := range t.CustomAudiences {
		if slices.ContainsFunc(t.ExcludedCustomAudiences, func(x TargetRef) bool { return x.ID == ref.ID }) {
			v.add("excludedCustomAudiences", "also_included")
		}
	}
	if t.AdvantageAudience && (t.AgeMax != maxAge || t.AgeMin < restrictedMinAge || t.AgeMin > advantageMaxAgeMin) {
		v.add("age", "advantage_audience_limits")
	}
	if restricted {
		if t.AgeMin != restrictedMinAge || t.AgeMax != maxAge {
			v.add("age", "restricted_category")
		}
		if len(t.Genders) > 0 {
			v.add("genders", "restricted_category")
		}
		if len(t.ExcludedCustomAudiences) > 0 || len(t.ExcludedLocations) > 0 {
			v.add("exclusions", "restricted_category")
		}
	}
}

func checkRefs(v issues, field string, refs []TargetRef) {
	if len(refs) > maxTargetRefs {
		v.add(field, "too_many")
	}
	for _, r := range refs {
		if strings.TrimSpace(r.ID) == "" {
			v.add(field, "invalid")
			return
		}
	}
}

func checkLocation(v issues, loc GeoLocation, restricted bool) {
	if strings.TrimSpace(loc.Key) == "" {
		v.add("", "invalid")
		return
	}
	switch loc.Kind {
	case LocationCountry:
		if len(loc.Key) != 2 {
			v.add("", "invalid")
		}
	case LocationRegion:
	case LocationCity:
		if loc.RadiusKm != 0 && (loc.RadiusKm < minCityRadiusKm || loc.RadiusKm > maxCityRadiusKm) {
			v.add("", "radius_out_of_range")
		}
		if restricted && loc.RadiusKm < restrictedRadiusKm {
			v.add("", "restricted_category")
		}
	default:
		v.add("", "invalid")
	}
}

func (p Placements) validate(v issues, d Destination) {
	if p.Automatic {
		if len(p.Platforms) > 0 || len(p.Positions) > 0 {
			v.add("", "automatic_with_manual")
		}
		return
	}
	if len(p.Platforms) == 0 {
		v.add("platforms", "required")
	}
	for _, platform := range p.Platforms {
		if _, ok := platformPositions[platform]; !ok {
			v.add("platforms", "invalid")
		}
	}
	for platform, positions := range p.Positions {
		allowed, ok := platformPositions[platform]
		if !ok || !slices.Contains(p.Platforms, platform) {
			v.add("positions", "invalid")
			continue
		}
		for _, pos := range positions {
			if !slices.Contains(allowed, pos) {
				v.add("positions", "invalid")
			}
		}
	}
	for _, device := range p.Devices {
		if device != DeviceMobile && device != DeviceDesktop {
			v.add("devices", "invalid")
		}
	}
	if len(p.Platforms) == 1 && p.Platforms[0] == PlatformAudienceNetwork {
		v.add("platforms", "audience_network_alone")
	}
	for platform, positions := range p.Positions {
		if len(positions) == 1 && positions[0] == "story" {
			v.add("positions", "story_alone")
		}
		if platform == PlatformFacebook && !slices.Contains(positions, "feed") && slices.ContainsFunc(positions, func(pos string) bool { return slices.Contains(positionsNeedingFeed, pos) }) {
			v.add("positions", "needs_feed")
		}
	}
	if d == DestinationInstagramDirect && !p.Includes(PlatformInstagram) {
		v.add("platforms", "instagram_required")
	}
	if d == DestinationMessenger && !p.Includes(PlatformFacebook) && !p.Includes(PlatformMessenger) {
		v.add("platforms", "facebook_required")
	}
}

func ValidHTTPSURL(raw string) bool {
	parsed, err := url.Parse(strings.TrimSpace(raw))
	return err == nil && parsed.Scheme == "https" && parsed.Host != "" && strings.Contains(parsed.Host, ".")
}
