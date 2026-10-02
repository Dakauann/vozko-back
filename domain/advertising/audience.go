package advertising

import (
	"errors"
	"strings"
	"time"

	"vozko/domain/crmfilter"
)

var (
	ErrAudienceTermsNotAccepted = errors.New("the ad account has not accepted meta's custom audience terms")
	ErrAudienceNotFound         = errors.New("audience not found")
	ErrNoCustomersMatched       = errors.New("no customer had an email or phone that meta can match")
)

type AudienceKind string

const (
	AudienceCustomerList AudienceKind = "CUSTOMER_LIST"
	AudienceLookalike    AudienceKind = "LOOKALIKE"
	AudienceWebsite      AudienceKind = "WEBSITE"
	AudienceEngagement   AudienceKind = "ENGAGEMENT"
	AudienceOther        AudienceKind = "OTHER"
)

const audienceReadyCode = 200

type Audience struct {
	MetaID               string       `json:"metaId"`
	Name                 string       `json:"name"`
	Description          string       `json:"description,omitempty"`
	Kind                 AudienceKind `json:"kind"`
	ApproxLower          int64        `json:"approxLower"`
	ApproxUpper          int64        `json:"approxUpper"`
	DeliveryCode         int          `json:"deliveryCode"`
	DeliveryDescription  string       `json:"deliveryDescription,omitempty"`
	OperationCode        int          `json:"operationCode"`
	OperationDescription string       `json:"operationDescription,omitempty"`
	OriginAudienceID     string       `json:"originAudienceId,omitempty"`
	LookalikeCountry     string       `json:"lookalikeCountry,omitempty"`
	LookalikeRatio       float64      `json:"lookalikeRatio,omitempty"`
	RetentionDays        int          `json:"retentionDays,omitempty"`
	CreatedTime          *time.Time   `json:"createdTime,omitempty"`
	UpdatedTime          *time.Time   `json:"updatedTime,omitempty"`
}

func (a Audience) Ready() bool { return a.DeliveryCode == audienceReadyCode }

type CustomerSource string

const (
	SourceCRM  CustomerSource = "crm"
	SourceFile CustomerSource = "file"
)

type CustomerListDraft struct {
	AdAccountID string           `json:"adAccountId"`
	Name        string           `json:"name"`
	Description string           `json:"description,omitempty"`
	Source      CustomerSource   `json:"source"`
	FileMediaID string           `json:"fileMediaId,omitempty"`
	SkipHeader  bool             `json:"skipHeader,omitempty"`
	Columns     []MatchKey       `json:"columns,omitempty"`
	CRMFilter   crmfilter.Filter `json:"crmFilter"`
}

const maxDescriptionRunes = 100

func (d CustomerListDraft) Validate() error {
	v := newIssues()
	if strings.TrimSpace(d.AdAccountID) == "" {
		v.add("adAccountId", "required")
	}
	v.text("name", d.Name, true, maxNameRunes)
	v.text("description", d.Description, false, maxDescriptionRunes)
	switch d.Source {
	case SourceCRM:
	case SourceFile:
		if strings.TrimSpace(d.FileMediaID) == "" {
			v.add("fileMediaId", "required")
		}
		identifying := false
		for _, c := range d.Columns {
			if !c.Valid() && c != "" {
				v.add("columns", "invalid")
			}
			identifying = identifying || c == MatchEmail || c == MatchPhone || c == MatchExternalID
		}
		if !identifying {
			v.add("columns", "needs_email_or_phone")
		}
	default:
		v.add("source", "invalid")
	}
	return v.err()
}

type LookalikeDraft struct {
	AdAccountID      string `json:"adAccountId"`
	Name             string `json:"name"`
	OriginAudienceID string `json:"originAudienceId"`
	Percent          int    `json:"percent"`
}

const (
	minLookalikePercent = 1
	maxLookalikePercent = 10
)

func (d LookalikeDraft) Ratio() float64 { return float64(d.Percent) / 100 }

func (d LookalikeDraft) Validate() error {
	v := newIssues()
	if strings.TrimSpace(d.AdAccountID) == "" {
		v.add("adAccountId", "required")
	}
	v.text("name", d.Name, true, maxNameRunes)
	if strings.TrimSpace(d.OriginAudienceID) == "" {
		v.add("originAudienceId", "required")
	}
	if d.Percent < minLookalikePercent || d.Percent > maxLookalikePercent {
		v.add("percent", "invalid")
	}
	return v.err()
}

type SavedAudience struct {
	ID          string     `json:"id"`
	WorkspaceID string     `json:"-"`
	Name        string     `json:"name"`
	Targeting   Targeting  `json:"targeting"`
	Placements  Placements `json:"placements"`
	CreatedBy   string     `json:"-"`
	CreatedAt   time.Time  `json:"createdAt"`
	UpdatedAt   time.Time  `json:"updatedAt"`
}

func (s *SavedAudience) Normalize() {
	s.Name = strings.TrimSpace(s.Name)
	s.Targeting.Normalize()
	if !s.Placements.Automatic && len(s.Placements.Platforms) == 0 {
		s.Placements = Placements{Automatic: true}
	}
}

func (s SavedAudience) Validate() error {
	v := newIssues()
	v.text("name", s.Name, true, maxNameRunes)
	s.Targeting.validate(v.at("targeting"), false)
	s.Placements.validate(v.at("placements"), "")
	return v.err()
}

const customAudienceBatch = 10_000

func Batches[T any](rows []T, size int) [][]T {
	if size <= 0 {
		size = customAudienceBatch
	}
	var out [][]T
	for start := 0; start < len(rows); start += size {
		out = append(out, rows[start:min(start+size, len(rows))])
	}
	return out
}

func CustomerBatchSize() int { return customAudienceBatch }
