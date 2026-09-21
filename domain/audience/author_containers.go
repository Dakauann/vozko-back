package audience

import (
	"fmt"
	"strings"
	"time"

	"vozko/domain/shared"
)

type AuthorContainersInput struct {
	WorkspaceID      string
	Source           Source
	AccountID        string
	AuthorExternalID string

	From *time.Time
	To   *time.Time

	Options shared.QueryOptions
}

func (in *AuthorContainersInput) Normalize() {
	in.WorkspaceID = strings.TrimSpace(in.WorkspaceID)
	in.AccountID = strings.TrimSpace(in.AccountID)
	in.AuthorExternalID = strings.TrimSpace(in.AuthorExternalID)
	in.Options.Pagination = shared.NormalizePagination(in.Options.Pagination)
}

func (in AuthorContainersInput) Validate() error {
	if in.WorkspaceID == "" {
		return fmt.Errorf("%w: workspace id is required", ErrInvalidFilter)
	}
	if in.From != nil && in.To != nil && in.From.After(*in.To) {
		return fmt.Errorf("%w: date range is inverted", ErrInvalidFilter)
	}
	if in.AuthorExternalID == "" {
		return fmt.Errorf("%w: author is required", ErrInvalidFilter)
	}
	if in.Source != "" && !in.Source.Valid() {
		return fmt.Errorf("%w: source %q", ErrInvalidFilter, in.Source)
	}
	return nil
}

type AuthorContainer struct {
	Source      Source `json:"source"`
	AccountID   string `json:"accountId"`
	ContainerID string `json:"containerId"`

	Comments          int       `json:"comments"`
	Stances           StanceMix `json:"stances"`
	SeverityMax       int       `json:"severityMax"`
	SeverityHighCount int       `json:"severityHighCount"`

	FirstOccurredAt time.Time `json:"firstOccurredAt"`
	LastOccurredAt  time.Time `json:"lastOccurredAt"`

	DerivedStance Stance `json:"derivedStance"`
	Reputation    int    `json:"reputation"`
}

func (c *AuthorContainer) Derive() {
	c.DerivedStance = DerivedStance(c.Stances)
	c.Reputation = AuthorReputation(c.Stances, c.SeverityHighCount)
}
