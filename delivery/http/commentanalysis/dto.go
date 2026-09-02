package commentanalysis

import (
	"time"

	"vozko/domain/comment_analysis"
)

// Wire shapes. The front-end's lib/comment-analysis/types.ts mirrors these
// one to one; a field added here is a field added there.

type CommentResponse struct {
	ID              string  `json:"id"`
	Source          string  `json:"source"`
	AccountID       string  `json:"accountId"`
	ContainerID     string  `json:"containerId"`
	SourceCommentID string  `json:"sourceCommentId"`
	ParentCommentID *string `json:"parentCommentId,omitempty"`

	AuthorExternalID string `json:"authorExternalId"`
	AuthorHandle     string `json:"authorHandle,omitempty"`

	Status        string `json:"status"`
	Attempts      int    `json:"attempts"`
	FailureReason string `json:"failureReason,omitempty"`

	Sentiment string `json:"sentiment,omitempty"`
	Stance    string `json:"stance,omitempty"`
	Intent    string `json:"intent,omitempty"`
	TopicKey  string `json:"topicKey,omitempty"`
	IsSpam    bool   `json:"isSpam"`
	Language  string `json:"language,omitempty"`

	Toxicity       string `json:"toxicity,omitempty"`
	PersonalAttack string `json:"personalAttack,omitempty"`
	LegalRisk      string `json:"legalRisk,omitempty"`
	Severity       int    `json:"severity"`
	RequiresAction bool   `json:"requiresAction"`

	Excerpt   string `json:"excerpt"`
	Truncated bool   `json:"truncated"`

	Model       string     `json:"model,omitempty"`
	AnalyzedAt  *time.Time `json:"analyzedAt,omitempty"`
	CommentedAt time.Time  `json:"commentedAt"`
	CreatedAt   time.Time  `json:"createdAt"`
}

func toCommentResponse(a *comment_analysis.CommentAnalysis) CommentResponse {
	return CommentResponse{
		ID: a.ID, Source: string(a.Source), AccountID: a.AccountID, ContainerID: a.ContainerID,
		SourceCommentID: a.SourceCommentID, ParentCommentID: a.ParentCommentID,
		AuthorExternalID: a.AuthorExternalID, AuthorHandle: a.AuthorHandle,
		Status: string(a.Status), Attempts: a.Attempts, FailureReason: a.FailureReason,
		Sentiment: string(a.Sentiment), Stance: string(a.Stance), Intent: string(a.Intent), TopicKey: a.TopicKey,
		IsSpam: a.IsSpam, Language: a.Language,
		Toxicity: string(a.Toxicity), PersonalAttack: string(a.PersonalAttack), LegalRisk: string(a.LegalRisk),
		Severity: a.Severity, RequiresAction: a.RequiresAction,
		Excerpt: a.Excerpt, Truncated: a.Truncated,
		Model: a.Model, AnalyzedAt: a.AnalyzedAt, CommentedAt: a.CommentedAt, CreatedAt: a.CreatedAt,
	}
}

// StatsResponse is the live aggregate; Counters flatten into it.
type StatsResponse struct {
	comment_analysis.Counters
	Topics          []comment_analysis.TopicStat `json:"topics"`
	AcceptanceScore int                          `json:"acceptanceScore"`
}

func toStatsResponse(s *comment_analysis.Stats) StatsResponse {
	topics := s.Topics
	if topics == nil {
		topics = []comment_analysis.TopicStat{}
	}
	return StatsResponse{Counters: s.Counters, Topics: topics, AcceptanceScore: s.AcceptanceScore}
}

type TrendPointResponse struct {
	BucketDate      string `json:"bucketDate"` // YYYY-MM-DD, UTC
	AcceptanceScore int    `json:"acceptanceScore"`
	comment_analysis.Counters
}

func toTrendPoint(r *comment_analysis.Rollup) TrendPointResponse {
	return TrendPointResponse{BucketDate: r.BucketDate.Format("2006-01-02"), AcceptanceScore: r.AcceptanceScore, Counters: r.Counters}
}

type AuthorResponse struct {
	ID               string                        `json:"id"`
	Source           string                        `json:"source"`
	AccountID        string                        `json:"accountId"`
	AuthorExternalID string                        `json:"authorExternalId"`
	AuthorHandle     string                        `json:"authorHandle,omitempty"`
	FirstSeenAt      time.Time                     `json:"firstSeenAt"`
	LastSeenAt       time.Time                     `json:"lastSeenAt"`
	Counters         comment_analysis.Counters     `json:"counters"`
	TopTopics        []comment_analysis.TopicCount `json:"topTopics"`
	DerivedStance    string                        `json:"derivedStance"`
	IsFlagged        bool                          `json:"isFlagged"`
	ModerationState  string                        `json:"moderationState"`
	UpdatedAt        time.Time                     `json:"updatedAt"`
}

func toAuthorResponse(a *comment_analysis.AuthorStats) AuthorResponse {
	top := a.TopTopics
	if top == nil {
		top = []comment_analysis.TopicCount{}
	}
	return AuthorResponse{
		ID: a.ID, Source: string(a.Source), AccountID: a.AccountID,
		AuthorExternalID: a.AuthorExternalID, AuthorHandle: a.AuthorHandle,
		FirstSeenAt: a.FirstSeenAt, LastSeenAt: a.LastSeenAt, Counters: a.Counters, TopTopics: top,
		DerivedStance: string(a.DerivedStance), IsFlagged: a.IsFlagged, ModerationState: string(a.ModerationState),
		UpdatedAt: a.UpdatedAt,
	}
}

type AuthorDetailResponse struct {
	Author   AuthorResponse    `json:"author"`
	Comments []CommentResponse `json:"comments"`
	Page     int               `json:"page"`
	PageSize int               `json:"pageSize"`
	Total    int64             `json:"total"`
}

type ModerationRequest struct {
	State string `json:"state"`
}

type SettingsResponse struct {
	Source            string                   `json:"source"`
	AccountID         string                   `json:"accountId"`
	Enabled           bool                     `json:"enabled"`
	Model             string                   `json:"model,omitempty"`
	Vertical          string                   `json:"vertical"`
	Topics            []comment_analysis.Topic `json:"topics"`
	SeverityThreshold int                      `json:"severityThreshold"`
	DailyCap          int                      `json:"dailyCap"`
	Instructions      string                   `json:"instructions,omitempty"`
	UpdatedAt         time.Time                `json:"updatedAt"`
}

func toSettingsResponse(s *comment_analysis.Settings) SettingsResponse {
	topics := []comment_analysis.Topic(s.Topics)
	if topics == nil {
		topics = []comment_analysis.Topic{}
	}
	return SettingsResponse{
		Source: string(s.Source), AccountID: s.AccountID, Enabled: s.Enabled, Model: s.Model,
		Vertical: string(s.Vertical), Topics: topics, SeverityThreshold: s.ActionPolicy.SeverityThreshold,
		DailyCap: s.DailyCap, Instructions: s.Instructions, UpdatedAt: s.UpdatedAt,
	}
}

// SettingsRequest is a PATCH-shaped update: absent fields are untouched.
type SettingsRequest struct {
	Enabled           *bool                     `json:"enabled,omitempty"`
	Model             *string                   `json:"model,omitempty"`
	Vertical          *string                   `json:"vertical,omitempty"`
	Topics            *[]comment_analysis.Topic `json:"topics,omitempty"`
	SeverityThreshold *int                      `json:"severityThreshold,omitempty"`
	DailyCap          *int                      `json:"dailyCap,omitempty"`
	Instructions      *string                   `json:"instructions,omitempty"`
}

type SpendResponse struct {
	comment_analysis.BatchTotals
}

type BackfillEstimateResponse struct {
	Containers        int   `json:"containers"`
	EstimatedComments int   `json:"estimatedComments"`
	EstimatedMicros   int64 `json:"estimatedMicros"`
}

type BackfillRequest struct {
	ContainerID       string `json:"containerId,omitempty"`
	ConfirmedEstimate int    `json:"confirmedEstimate"`
}

type BackfillResponse struct {
	ID                string     `json:"id"`
	Source            string     `json:"source"`
	AccountID         string     `json:"accountId"`
	ContainerID       string     `json:"containerId,omitempty"`
	Status            string     `json:"status"`
	EstimatedComments int        `json:"estimatedComments"`
	Fetched           int        `json:"fetched"`
	Enqueued          int        `json:"enqueued"`
	Progress          float64    `json:"progress"`
	Error             string     `json:"error,omitempty"`
	CreatedAt         time.Time  `json:"createdAt"`
	UpdatedAt         time.Time  `json:"updatedAt"`
	FinishedAt        *time.Time `json:"finishedAt,omitempty"`
}

func toBackfillResponse(b *comment_analysis.Backfill) BackfillResponse {
	return BackfillResponse{
		ID: b.ID, Source: string(b.Source), AccountID: b.AccountID, ContainerID: b.ContainerID, Status: string(b.Status),
		EstimatedComments: b.EstimatedComments, Fetched: b.Fetched, Enqueued: b.Enqueued, Progress: b.Progress(),
		Error: b.Error, CreatedAt: b.CreatedAt, UpdatedAt: b.UpdatedAt, FinishedAt: b.FinishedAt,
	}
}

// ---- per-post settings ----

// ContainerOverrideRequest is PUT-shaped: it replaces the post's override.
// A null or absent field means "inherit from the account".
type ContainerOverrideRequest struct {
	Enabled           *bool                     `json:"enabled"`
	Model             *string                   `json:"model"`
	Topics            *[]comment_analysis.Topic `json:"topics"`
	SeverityThreshold *int                      `json:"severityThreshold"`
	Instructions      *string                   `json:"instructions"`
}

type ContainerOverrideResponse struct {
	ContainerID       string                    `json:"containerId"`
	Enabled           *bool                     `json:"enabled,omitempty"`
	Model             *string                   `json:"model,omitempty"`
	Topics            *[]comment_analysis.Topic `json:"topics,omitempty"`
	SeverityThreshold *int                      `json:"severityThreshold,omitempty"`
	Instructions      *string                   `json:"instructions,omitempty"`
	UpdatedAt         time.Time                 `json:"updatedAt"`
}

// ContainerSettingsResponse is what the post editor renders: the override as
// stored (absent when the post inherits everything) and the effective result.
type ContainerSettingsResponse struct {
	Override  *ContainerOverrideResponse `json:"override,omitempty"`
	Effective SettingsResponse           `json:"effective"`
}

func toContainerSettingsResponse(cs *comment_analysis.ContainerSettings) ContainerSettingsResponse {
	out := ContainerSettingsResponse{Effective: toSettingsResponse(&cs.Effective)}
	if o := cs.Override; o != nil {
		r := &ContainerOverrideResponse{
			ContainerID: o.ContainerID, Enabled: o.Enabled, Model: o.Model,
			SeverityThreshold: o.SeverityThreshold, Instructions: o.Instructions, UpdatedAt: o.UpdatedAt,
		}
		if o.Topics != nil {
			t := []comment_analysis.Topic(*o.Topics)
			r.Topics = &t
		}
		out.Override = r
	}
	return out
}
