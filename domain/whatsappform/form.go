package whatsappform

import (
	"encoding/json"
	"slices"
	"strings"
	"time"
	"unicode/utf8"

	"vozko/domain/shared"
)

const (
	MaxNameLength        = 120
	MaxDescriptionLength = 500
	DefaultPageSize      = 50
	MaxPageSize          = 100
	MaxSearchLength      = 120
)

type FormKind string

const (
	KindBuilt    FormKind = "built"
	KindExternal FormKind = "external"
)

type Form struct {
	ID                 string     `json:"id"`
	WorkspaceID        string     `json:"workspaceId"`
	Name               string     `json:"name"`
	Description        string     `json:"description,omitempty"`
	Kind               FormKind   `json:"kind"`
	DraftVersionID     string     `json:"draftVersionId"`
	PublishedVersionID string     `json:"publishedVersionId,omitempty"`
	CreatedBy          string     `json:"createdBy,omitempty"`
	CreatedAt          time.Time  `json:"createdAt"`
	UpdatedAt          time.Time  `json:"updatedAt"`
	ArchivedAt         *time.Time `json:"archivedAt,omitempty"`
}

func NormalizeName(name string) (string, error) {
	name = strings.Join(strings.Fields(name), " ")
	switch {
	case name == "":
		return "", ErrNameRequired
	case utf8.RuneCountInString(name) > MaxNameLength:
		return "", ErrNameTooLong
	}
	return name, nil
}

func (f *Form) EnsureActive() error {
	if f.ArchivedAt != nil {
		return ErrFormArchived
	}
	return nil
}

type Version struct {
	ID              string          `json:"id"`
	WorkspaceID     string          `json:"workspaceId"`
	FormID          string          `json:"formId"`
	Number          int             `json:"number"`
	Document        Document        `json:"document"`
	FlowJSON        json.RawMessage `json:"-"`
	FlowVersion     string          `json:"flowVersion,omitempty"`
	CompilerVersion string          `json:"compilerVersion,omitempty"`
	Hash            string          `json:"hash,omitempty"`
	Problems        Problems        `json:"problems,omitempty"`
	CreatedBy       string          `json:"createdBy,omitempty"`
	CreatedAt       time.Time       `json:"createdAt"`
}

func (v Version) Compiled() bool {
	return len(v.FlowJSON) > 0 && v.Hash != "" && len(v.Problems) == 0
}

type ApplyStatus string

const (
	ApplyPending ApplyStatus = "pending"
	ApplyApplied ApplyStatus = "applied"
	ApplyPartial ApplyStatus = "partial"
	ApplySkipped ApplyStatus = "skipped"
)

type FileOutcome struct {
	Key     string `json:"key"`
	FileID  string `json:"fileId"`
	MediaID string `json:"mediaId,omitempty"`
	Problem string `json:"problem,omitempty"`
}

type ApplyReport struct {
	Changed   []string      `json:"changed,omitempty"`
	Conflicts []string      `json:"conflicts,omitempty"`
	Refused   []string      `json:"refused,omitempty"`
	Consent   string        `json:"consent,omitempty"`
	Files     []FileOutcome `json:"files,omitempty"`
	Written   bool          `json:"written,omitempty"`
	Resumed   string        `json:"resumed,omitempty"`
	Triggered bool          `json:"triggered,omitempty"`
}

type Response struct {
	ID               string          `json:"id"`
	WorkspaceID      string          `json:"workspaceId"`
	FormID           string          `json:"formId,omitempty"`
	VersionID        string          `json:"versionId,omitempty"`
	SendID           string          `json:"sendId,omitempty"`
	Origin           SendOrigin      `json:"origin,omitempty"`
	Match            MatchResult     `json:"match"`
	InboundMessageID string          `json:"inboundMessageId"`
	ConversationID   string          `json:"conversationId,omitempty"`
	EntryID          string          `json:"entryId,omitempty"`
	EntryType        string          `json:"entryType,omitempty"`
	LeadID           string          `json:"leadId,omitempty"`
	CampaignID       string          `json:"campaignId,omitempty"`
	BusinessPhoneID  string          `json:"businessPhoneId"`
	ContactNumber    string          `json:"contactNumber"`
	Answers          Answers         `json:"answers"`
	Raw              json.RawMessage `json:"-"`
	ApplyStatus      ApplyStatus     `json:"applyStatus"`
	Report           ApplyReport     `json:"report"`
	SubmittedAt      time.Time       `json:"submittedAt"`
	CreatedAt        time.Time       `json:"createdAt"`
}

type MatchFilter string

const (
	MatchFilterAny       MatchFilter = ""
	MatchFilterMatched   MatchFilter = "matched"
	MatchFilterUnmatched MatchFilter = "unmatched"
)

type ResponseQuery struct {
	WorkspaceID string
	FormID      string
	VersionIDs  []string
	From, To    *time.Time
	Origins     []SendOrigin
	PhoneIDs    []string
	CampaignID  string
	Match       MatchFilter
	Search      string
	After       string
	Limit       int
	Scope       ReadScope
}

type ReadScope struct {
	Resolved       bool
	Restrict       bool
	DepartmentIDs  []string
	AssigneeUserID string
}

func (q *ResponseQuery) Normalize() error {
	if strings.TrimSpace(q.WorkspaceID) == "" {
		return ErrWorkspaceRequired
	}
	if !q.Scope.Resolved {
		return ErrForbidden
	}
	q.Search = strings.TrimSpace(q.Search)
	switch {
	case q.Limit == 0:
		q.Limit = DefaultPageSize
	case q.Limit < 0 || q.Limit > MaxPageSize:
		return ErrListQueryInvalid
	}
	if q.From != nil && q.To != nil && q.From.After(*q.To) {
		return ErrListQueryInvalid
	}
	if utf8.RuneCountInString(q.Search) > MaxSearchLength {
		return ErrListQueryInvalid
	}
	for _, o := range q.Origins {
		if !o.Valid() {
			return ErrListQueryInvalid
		}
	}
	if !slices.Contains([]MatchFilter{MatchFilterAny, MatchFilterMatched, MatchFilterUnmatched}, q.Match) {
		return ErrListQueryInvalid
	}
	if _, _, err := shared.ParseKeyset(q.After); err != nil {
		return ErrListQueryInvalid
	}
	return nil
}

func (q ResponseQuery) Cursor() (shared.Keyset, bool) {
	key, paged, err := shared.ParseKeyset(q.After)
	if err != nil {
		return shared.Keyset{}, false
	}
	return key, paged
}

type Page[T any] struct {
	Items []T    `json:"items"`
	Next  string `json:"next,omitempty"`
}

func NormalizeDescription(description string) (string, error) {
	description = strings.TrimSpace(description)
	if utf8.RuneCountInString(description) > MaxDescriptionLength {
		return "", ErrDescriptionTooLong
	}
	return description, nil
}
