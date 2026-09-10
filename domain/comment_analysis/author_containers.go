package comment_analysis

import (
	"fmt"
	"strings"
	"time"

	"vozko/domain/shared"
)

// "Filtrar todos os posts que aquela pessoa comentou" (§2).
//
// The feed answers "who said what on this post"; this is the inverse, and the
// navigation the rest of the author view hangs off: given one person, which of
// our posts do they turn up on, how often, and how they behave on each.
//
// A deliberate limit, stated here because the UI must not overpromise it: we
// know about COMMENTS. A like is not in the comment webhook and is not stored,
// so "interagiu" here means "commented" and nothing else.

// AuthorContainersInput asks for the posts one author has commented on.
//
// The author is addressed by their external id rather than their handle:
// handles are renameable on every channel we mirror, and a rename would
// silently split one person's history in two.
type AuthorContainersInput struct {
	WorkspaceID      string
	Source           Source
	AccountID        string
	AuthorExternalID string

	// From and To narrow the answer to a window, by the comment's own
	// timestamp: "em quais posts esta pessoa comentou este mês".
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

// AuthorContainer is one post an author has commented on, with how they behaved
// on it.
//
// The repository fills the counts; the standing is derived here by the SAME
// functions that derive the author's overall standing, so "hostile on this
// post" and "hostile overall" cannot mean two different things.
type AuthorContainer struct {
	Source      Source `json:"source"`
	AccountID   string `json:"accountId"`
	ContainerID string `json:"containerId"`

	Comments          int       `json:"comments"`
	Stances           StanceMix `json:"stances"`
	SeverityMax       int       `json:"severityMax"`
	SeverityHighCount int       `json:"severityHighCount"`

	FirstCommentedAt time.Time `json:"firstCommentedAt"`
	LastCommentedAt  time.Time `json:"lastCommentedAt"`

	DerivedStance Stance `json:"derivedStance"`
	Reputation    int    `json:"reputation"`
}

// Derive computes this post's standing from its counts. Pure, like every other
// derived number in the engine: a rebuild lands on the same figures.
func (c *AuthorContainer) Derive() {
	c.DerivedStance = DerivedStance(c.Stances)
	c.Reputation = AuthorReputation(c.Stances, c.SeverityHighCount)
}
