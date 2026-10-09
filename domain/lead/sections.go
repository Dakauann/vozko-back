package lead

import (
	"cmp"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"slices"
	"strconv"
	"strings"
	"time"

	"vozko/domain/crmfilter"
	"vozko/domain/customfield"
	"vozko/domain/shared"
)

type Section string

const (
	SectionSummary Section = "summary"
	SectionFacets  Section = "facets"
	SectionPlaces  Section = "places"
)

const (
	MaxPlaceCities    = 50
	MaxPlaceDistricts = 100
	MaxFacetOwners    = 100
)

const sectionFingerprintVersion = "v3"

type SectionQuery struct {
	WorkspaceID       string
	Filter            crmfilter.Filter
	Today             time.Time
	ClassificationKey string
	PlacePrefix       string
}

func (q SectionQuery) ListInput() ListLeadsInput {
	return ListLeadsInput{WorkspaceID: q.WorkspaceID, Filter: q.Filter, Today: q.Today}
}

func (q SectionQuery) Key(section Section, v Viewer, areas string, parts ...string) SectionKey {
	return SectionKey{
		Section: string(section),
		Filter:  q.Filter,
		Today:   q.Today,
		Tier:    v.Tier(),
		Areas:   areas,
		Parts:   append([]string{q.ClassificationKey}, parts...),
	}
}

type SectionKey struct {
	Section string
	Filter  crmfilter.Filter
	Today   time.Time
	Tier    string
	Areas   string
	Parts   []string
}

func (k SectionKey) Fingerprint() (string, error) {
	filter, err := json.Marshal(k.Filter)
	if err != nil {
		return "", fmt.Errorf("fingerprint of the %s section: %w", k.Section, err)
	}
	parts := append([]string{
		sectionFingerprintVersion,
		k.Section,
		shared.DateOf(k.Today).String(),
		k.Tier,
		k.Areas,
		string(filter),
	}, k.Parts...)
	sum := sha256.Sum256([]byte(strings.Join(parts, "\x1f")))
	return hex.EncodeToString(sum[:]), nil
}

func (v Viewer) Tier() string {
	return "s" + strconv.FormatBool(v.Fields.ReadsSensitive) + ":a" + strconv.FormatBool(v.ReadsAddresses)
}

func ClassificationKeyFor(v Viewer) string {
	for _, def := range v.Definitions {
		if def != nil && def.Role == customfield.RoleClassification && customfield.VisibleTo(def, v.Fields) {
			return def.Key
		}
	}
	return ""
}

type SummarySection struct {
	Total          int64 `json:"total"`
	WithAddress    int64 `json:"withAddress"`
	OnMap          int64 `json:"onMap"`
	Approximate    int64 `json:"approximate"`
	WithoutAddress int64 `json:"withoutAddress"`
	NotFound       int64 `json:"notFound"`
	QuotaExceeded  int64 `json:"quotaExceeded"`
	Refused        int64 `json:"refused"`
	Pending        int64 `json:"pending"`
	BirthdaysToday int64 `json:"birthdaysToday"`
	Blocked        int64 `json:"blocked"`
	WindowOpen     int64 `json:"windowOpen"`
}

type OwnerCount struct {
	Owner string `json:"owner"`
	Name  string `json:"name,omitempty"`
	Count int64  `json:"count"`
}

type ClassificationFacet struct {
	Key    string           `json:"key"`
	Values map[string]int64 `json:"values"`
}

type FacetsSection struct {
	Channels         map[string]int64     `json:"channels"`
	MemoryCategories map[string]int64     `json:"memoryCategories"`
	CampaignStatuses map[string]int64     `json:"campaignStatuses"`
	Sources          map[string]int64     `json:"sources"`
	Owners           []OwnerCount         `json:"owners"`
	OwnersTruncated  bool                 `json:"ownersTruncated"`
	Classification   *ClassificationFacet `json:"classification,omitempty"`
}

func TopOwners(owners []OwnerCount) ([]OwnerCount, bool) {
	top := slices.Clone(owners)
	if top == nil {
		top = []OwnerCount{}
	}
	slices.SortFunc(top, func(a, b OwnerCount) int {
		if byCount := cmp.Compare(b.Count, a.Count); byCount != 0 {
			return byCount
		}
		return cmp.Compare(a.Owner, b.Owner)
	})
	if len(top) > MaxFacetOwners {
		return top[:MaxFacetOwners], true
	}
	return top, false
}

type CityCount struct {
	CityKey string `json:"cityKey"`
	City    string `json:"city"`
	State   string `json:"state"`
	Count   int64  `json:"count"`
}

type DistrictCount struct {
	Pair        string `json:"pair"`
	CityKey     string `json:"cityKey"`
	DistrictKey string `json:"districtKey"`
	District    string `json:"district"`
	City        string `json:"city"`
	State       string `json:"state"`
	Count       int64  `json:"count"`
}

type PlacesSection struct {
	Cities    []CityCount     `json:"cities"`
	Districts []DistrictCount `json:"districts"`
}

type SectionReader interface {
	ReadSummary(ctx context.Context, q SectionQuery) (*SummarySection, error)
	ReadFacets(ctx context.Context, q SectionQuery) (*FacetsSection, error)
	ReadPlaces(ctx context.Context, q SectionQuery) (*PlacesSection, error)
}
