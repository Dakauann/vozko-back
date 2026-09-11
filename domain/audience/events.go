package audience

import (
	"time"

	"vozko/domain/shared"
)

// The live feed (§7).
//
// "Feed ao vivo (broadcasting de comentários analisados ao vivo), e conforme
// vão sendo analisados."
//
// The shape of the problem is backpressure, not plumbing: a backfill classifies
// thousands of comments in minutes, and a socket that forwards each one drowns
// the browser. Two things prevent that, both here rather than in the transport:
//
//  1. The unit is a BATCH, not a comment. The engine already classifies in
//     batches and saves them together, so one event per batch is the natural
//     coalescing point and costs nothing to arrange.
//  2. A batch is truncated to MaxLiveEvents rows plus a count of the rest, so
//     the biggest possible message is bounded no matter how large the batch is.
//     A viewer sees the first few and a truthful "and N more", which is what a
//     live feed can usefully show anyway.

// MaxLiveEvents bounds one broadcast. Small on purpose: this is a ticker
// someone glances at, not a list they page through.
const MaxLiveEvents = 25

// CommentAnalyzed is one freshly classified comment, in the shape the feed
// draws. It carries the excerpt the engine already stores and no more; the full
// text still lives in the channel's own table.
type CommentAnalyzed struct {
	CommentID   string `json:"commentId"`
	WorkspaceID string `json:"workspaceId"`
	Source      Source `json:"source"`
	AccountID   string `json:"accountId"`
	ContainerID string `json:"containerId"`

	AuthorExternalID string `json:"authorExternalId"`
	AuthorHandle     string `json:"authorHandle,omitempty"`

	Stance    Stance           `json:"stance,omitempty"`
	Sentiment shared.Sentiment `json:"sentiment,omitempty"`
	Intent    Intent           `json:"intent,omitempty"`
	TopicKey  string           `json:"topicKey,omitempty"`

	Severity       int  `json:"severity"`
	RequiresAction bool `json:"requiresAction"`
	IsSpam         bool `json:"isSpam"`

	Excerpt    string    `json:"excerpt"`
	OccurredAt time.Time `json:"occurredAt"`
	AnalyzedAt time.Time `json:"analyzedAt"`
}

// AnalysisBatchAnalyzed is one broadcast: the rows to draw, and how many more
// were classified in the same batch but not sent.
type AnalysisBatchAnalyzed struct {
	WorkspaceID string            `json:"workspaceId"`
	Source      Source            `json:"source"`
	AccountID   string            `json:"accountId"`
	ContainerID string            `json:"containerId"`
	Items       []CommentAnalyzed `json:"items"`
	// More is the count omitted by the MaxLiveEvents cap. Shown as "and N
	// more" rather than dropped silently, so a live feed never implies a
	// backfill classified less than it did.
	More int `json:"more"`
}

// NewCommentAnalyzed projects a stored row onto the event. Only analysed rows
// belong in the feed: a pending or failed row has nothing to show.
func NewCommentAnalyzed(a *Analysis) CommentAnalyzed {
	e := CommentAnalyzed{
		CommentID:        a.ID,
		WorkspaceID:      a.WorkspaceID,
		Source:           a.Source,
		AccountID:        a.AccountID,
		ContainerID:      a.ContainerID,
		AuthorExternalID: a.AuthorExternalID,
		AuthorHandle:     a.AuthorHandle,
		Stance:           a.Stance,
		Sentiment:        a.Sentiment,
		Intent:           a.Intent,
		TopicKey:         a.TopicKey,
		Severity:         a.Severity,
		RequiresAction:   a.RequiresAction,
		IsSpam:           a.IsSpam,
		Excerpt:          a.Excerpt,
		OccurredAt:       a.OccurredAt,
	}
	if a.AnalyzedAt != nil {
		e.AnalyzedAt = *a.AnalyzedAt
	}
	return e
}

// NewAnalysisBatchAnalyzed builds one broadcast from the rows a batch just
// applied. Rows that were not analysed are skipped, and the result is nil when
// nothing is worth sending, so a caller can simply check for nil.
//
// The worst comments go FIRST and survive the cap. A feed that truncated by
// arrival order would, during a backfill, reliably drop the one row an operator
// needed to see.
func NewAnalysisBatchAnalyzed(rows []*Analysis) *AnalysisBatchAnalyzed {
	items := make([]CommentAnalyzed, 0, len(rows))
	var out AnalysisBatchAnalyzed
	for _, r := range rows {
		if r == nil || r.Status != StatusAnalyzed {
			continue
		}
		if out.WorkspaceID == "" {
			out.WorkspaceID, out.Source = r.WorkspaceID, r.Source
			out.AccountID, out.ContainerID = r.AccountID, r.ContainerID
		}
		items = append(items, NewCommentAnalyzed(r))
	}
	if len(items) == 0 {
		return nil
	}
	sortBySeverityDesc(items)
	if len(items) > MaxLiveEvents {
		out.More = len(items) - MaxLiveEvents
		items = items[:MaxLiveEvents]
	}
	out.Items = items
	return &out
}

// sortBySeverityDesc is an insertion sort: MaxLiveEvents is 25 and a batch is
// tens of rows, so this is cheaper than reaching for the sort package and it
// keeps equal-severity rows in the order they were classified.
func sortBySeverityDesc(items []CommentAnalyzed) {
	for i := 1; i < len(items); i++ {
		for j := i; j > 0 && items[j].Severity > items[j-1].Severity; j-- {
			items[j], items[j-1] = items[j-1], items[j]
		}
	}
}
