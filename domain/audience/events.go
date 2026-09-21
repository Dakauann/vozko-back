package audience

import (
	"time"

	"vozko/domain/shared"
)

const MaxLiveEvents = 25

type CommentAnalyzed struct {
	CommentID   string      `json:"commentId"`
	SubjectKind SubjectKind `json:"subjectKind"`
	WorkspaceID string      `json:"workspaceId"`
	Source      Source      `json:"source"`
	AccountID   string      `json:"accountId"`
	ContainerID string      `json:"containerId"`

	AuthorExternalID string `json:"authorExternalId"`
	AuthorHandle     string `json:"authorHandle,omitempty"`

	Stance    Stance           `json:"stance,omitempty"`
	Sentiment shared.Sentiment `json:"sentiment,omitempty"`
	Intent    Intent           `json:"intent,omitempty"`
	TopicKey  string           `json:"topicKey,omitempty"`

	Severity       int  `json:"severity"`
	RequiresAction bool `json:"requiresAction"`
	IsSpam         bool `json:"isSpam"`

	Excerpt           string        `json:"excerpt"`
	Interest          Interest      `json:"interest,omitempty"`
	ProductInterest   string        `json:"productInterest,omitempty"`
	Disposition       Disposition   `json:"disposition,omitempty"`
	Qualification     Qualification `json:"qualification,omitempty"`
	NextAction        NextAction    `json:"nextAction,omitempty"`
	Summary           string        `json:"summary,omitempty"`
	AttendanceQuality int           `json:"attendanceQuality,omitempty"`
	MessageCount      int           `json:"messageCount,omitempty"`
	OccurredAt        time.Time     `json:"occurredAt"`
	AnalyzedAt        time.Time     `json:"analyzedAt"`
}

type AnalysisBatchAnalyzed struct {
	WorkspaceID string            `json:"workspaceId"`
	Source      Source            `json:"source"`
	AccountID   string            `json:"accountId"`
	ContainerID string            `json:"containerId"`
	Items       []CommentAnalyzed `json:"items"`
	More        int               `json:"more"`
}

func NewCommentAnalyzed(a *Analysis) CommentAnalyzed {
	e := CommentAnalyzed{
		CommentID:         a.ID,
		SubjectKind:       a.Kind(),
		WorkspaceID:       a.WorkspaceID,
		Source:            a.Source,
		AccountID:         a.AccountID,
		ContainerID:       a.ContainerID,
		AuthorExternalID:  a.AuthorExternalID,
		AuthorHandle:      a.AuthorHandle,
		Stance:            a.Stance,
		Sentiment:         a.Sentiment,
		Intent:            a.Intent,
		TopicKey:          a.TopicKey,
		Severity:          a.Severity,
		RequiresAction:    a.RequiresAction,
		IsSpam:            a.IsSpam,
		Excerpt:           a.Excerpt,
		Interest:          a.Interest,
		ProductInterest:   a.ProductInterest,
		Disposition:       a.Disposition,
		Qualification:     a.Qualification,
		NextAction:        a.NextAction,
		Summary:           a.Summary,
		AttendanceQuality: a.AttendanceQuality,
		MessageCount:      a.MessageCount,
		OccurredAt:        a.OccurredAt,
	}
	if a.AnalyzedAt != nil {
		e.AnalyzedAt = *a.AnalyzedAt
	}
	return e
}

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

func sortBySeverityDesc(items []CommentAnalyzed) {
	for i := 1; i < len(items); i++ {
		for j := i; j > 0 && items[j].Severity > items[j-1].Severity; j-- {
			items[j], items[j-1] = items[j-1], items[j]
		}
	}
}
