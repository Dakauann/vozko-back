package audience

import (
	"time"

	"vozko/domain/audience"
)

type CommentResponse struct {
	ID              string  `json:"id"`
	SubjectKind     string  `json:"subjectKind"`
	Source          string  `json:"source"`
	AccountID       string  `json:"accountId"`
	ContainerID     string  `json:"containerId"`
	SubjectID       string  `json:"subjectId"`
	ParentSubjectID *string `json:"parentCommentId,omitempty"`

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

	Interest          string `json:"interest,omitempty"`
	ProductInterest   string `json:"productInterest,omitempty"`
	Disposition       string `json:"disposition,omitempty"`
	Qualification     string `json:"qualification,omitempty"`
	NextAction        string `json:"nextAction,omitempty"`
	Summary           string `json:"summary,omitempty"`
	AttendanceQuality int    `json:"attendanceQuality,omitempty"`
	MessageCount      int    `json:"messageCount,omitempty"`

	Model      string     `json:"model,omitempty"`
	AnalyzedAt *time.Time `json:"analyzedAt,omitempty"`
	OccurredAt time.Time  `json:"occurredAt"`
	CreatedAt  time.Time  `json:"createdAt"`
}

func toCommentResponse(a *audience.Analysis) CommentResponse {
	return CommentResponse{
		ID: a.ID, SubjectKind: string(a.Kind()),
		Source: string(a.Source), AccountID: a.AccountID, ContainerID: a.ContainerID,
		SubjectID: a.SubjectID, ParentSubjectID: a.ParentSubjectID,
		AuthorExternalID: a.AuthorExternalID, AuthorHandle: a.AuthorHandle,
		Status: string(a.Status), Attempts: a.Attempts, FailureReason: a.FailureReason,
		Sentiment: string(a.Sentiment), Stance: string(a.Stance), Intent: string(a.Intent), TopicKey: a.TopicKey,
		IsSpam: a.IsSpam, Language: a.Language,
		Toxicity: string(a.Toxicity), PersonalAttack: string(a.PersonalAttack), LegalRisk: string(a.LegalRisk),
		Severity: a.Severity, RequiresAction: a.RequiresAction,
		Excerpt: a.Excerpt, Truncated: a.Truncated,
		Interest: string(a.Interest), ProductInterest: a.ProductInterest,
		Disposition: string(a.Disposition), Qualification: string(a.Qualification),
		NextAction: string(a.NextAction), Summary: a.Summary,
		AttendanceQuality: a.AttendanceQuality, MessageCount: a.MessageCount,
		Model: a.Model, AnalyzedAt: a.AnalyzedAt, OccurredAt: a.OccurredAt, CreatedAt: a.CreatedAt,
	}
}

type StatsResponse struct {
	audience.Counters
	Topics          []audience.TopicStat    `json:"topics"`
	Subjects        []audience.SubjectCount `json:"subjects"`
	AcceptanceScore int                     `json:"acceptanceScore"`
}

func toStatsResponse(s *audience.Stats) StatsResponse {
	topics := s.Topics
	if topics == nil {
		topics = []audience.TopicStat{}
	}
	subjects := s.Subjects
	if subjects == nil {
		subjects = []audience.SubjectCount{}
	}
	return StatsResponse{
		Counters: s.Counters, Topics: topics, Subjects: subjects,
		AcceptanceScore: s.AcceptanceScore,
	}
}

type TrendPointResponse struct {
	BucketDate      string `json:"bucketDate"`
	AcceptanceScore int    `json:"acceptanceScore"`
	audience.Counters
}

func toTrendPoint(r *audience.Rollup) TrendPointResponse {
	return TrendPointResponse{BucketDate: r.BucketDate.Format("2006-01-02"), AcceptanceScore: r.AcceptanceScore, Counters: r.Counters}
}

type AuthorResponse struct {
	ID               string                       `json:"id"`
	Source           string                       `json:"source"`
	AccountID        string                       `json:"accountId"`
	AuthorExternalID string                       `json:"authorExternalId"`
	AuthorHandle     string                       `json:"authorHandle,omitempty"`
	FirstSeenAt      time.Time                    `json:"firstSeenAt"`
	LastSeenAt       time.Time                    `json:"lastSeenAt"`
	Counters         audience.Counters            `json:"counters"`
	TopTopics        []audience.TopicCount        `json:"topTopics"`
	DerivedStance    string                       `json:"derivedStance"`
	Reputation       int                          `json:"reputation"`
	Role             audience.AuthorRoleInference `json:"role"`
	RoleDisplayable  bool                         `json:"roleDisplayable"`
	IsFlagged        bool                         `json:"isFlagged"`
	ModerationState  string                       `json:"moderationState"`
	UpdatedAt        time.Time                    `json:"updatedAt"`
}

func toAuthorResponse(a *audience.AuthorStats) AuthorResponse {
	top := a.TopTopics
	if top == nil {
		top = []audience.TopicCount{}
	}
	return AuthorResponse{
		ID: a.ID, Source: string(a.Source), AccountID: a.AccountID,
		AuthorExternalID: a.AuthorExternalID, AuthorHandle: a.AuthorHandle,
		FirstSeenAt: a.FirstSeenAt, LastSeenAt: a.LastSeenAt, Counters: a.Counters, TopTopics: top,
		DerivedStance: string(a.DerivedStance), Reputation: a.Reputation,
		Role: a.Role, RoleDisplayable: a.Role.Displayable(),
		IsFlagged: a.IsFlagged, ModerationState: string(a.ModerationState),
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

type EscalateRequest struct {
	RecipientID   string `json:"recipientId"`
	RecipientKind string `json:"recipientKind,omitempty"`
	Note          string `json:"note,omitempty"`
}

type ReplyRequest struct {
	Text string `json:"text"`
}

type EscalationResponse struct {
	CommentID string `json:"commentId"`
	Author    string `json:"author"`
	Where     string `json:"where"`
	Text      string `json:"text"`
	SentAt    string `json:"sentAt"`
}

func toEscalationResponse(e audience.Escalation) EscalationResponse {
	return EscalationResponse{
		CommentID: e.CommentID, Author: e.Author(), Where: e.Where(),
		Text: e.Message(), SentAt: time.Now().UTC().Format(time.RFC3339),
	}
}

type SettingsResponse struct {
	Source            string               `json:"source"`
	AccountID         string               `json:"accountId"`
	Enabled           bool                 `json:"enabled"`
	Model             string               `json:"model,omitempty"`
	Vertical          string               `json:"vertical"`
	Topics            []audience.Topic     `json:"topics"`
	SeverityThreshold int                  `json:"severityThreshold"`
	DailyCap          int                  `json:"dailyCap"`
	Instructions      string               `json:"instructions,omitempty"`
	ReplyPolicy       audience.ReplyPolicy `json:"replyPolicy"`
	UpdatedAt         time.Time            `json:"updatedAt"`
}

func toSettingsResponse(s *audience.Settings) SettingsResponse {
	topics := []audience.Topic(s.Topics)
	if topics == nil {
		topics = []audience.Topic{}
	}
	return SettingsResponse{
		Source: string(s.Source), AccountID: s.AccountID, Enabled: s.Enabled, Model: s.Model,
		Vertical: string(s.Vertical), Topics: topics, SeverityThreshold: s.ActionPolicy.SeverityThreshold,
		ReplyPolicy: s.ReplyPolicy,
		DailyCap:    s.DailyCap, Instructions: s.Instructions, UpdatedAt: s.UpdatedAt,
	}
}

type SettingsRequest struct {
	Enabled           *bool                 `json:"enabled,omitempty"`
	Model             *string               `json:"model,omitempty"`
	Vertical          *string               `json:"vertical,omitempty"`
	Topics            *[]audience.Topic     `json:"topics,omitempty"`
	SeverityThreshold *int                  `json:"severityThreshold,omitempty"`
	DailyCap          *int                  `json:"dailyCap,omitempty"`
	Instructions      *string               `json:"instructions,omitempty"`
	ReplyPolicy       *audience.ReplyPolicy `json:"replyPolicy,omitempty"`
}

type SpendResponse struct {
	audience.BatchTotals
}

type BackfillEstimateResponse struct {
	Containers        int `json:"containers"`
	EstimatedComments int `json:"estimatedComments"`
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

func toBackfillResponse(b *audience.Backfill) BackfillResponse {
	return BackfillResponse{
		ID: b.ID, Source: string(b.Source), AccountID: b.AccountID, ContainerID: b.ContainerID, Status: string(b.Status),
		EstimatedComments: b.EstimatedComments, Fetched: b.Fetched, Enqueued: b.Enqueued, Progress: b.Progress(),
		Error: b.Error, CreatedAt: b.CreatedAt, UpdatedAt: b.UpdatedAt, FinishedAt: b.FinishedAt,
	}
}

type ContainerOverrideRequest struct {
	Enabled           *bool             `json:"enabled"`
	Model             *string           `json:"model"`
	Topics            *[]audience.Topic `json:"topics"`
	SeverityThreshold *int              `json:"severityThreshold"`
	Instructions      *string           `json:"instructions"`
}

type ContainerOverrideResponse struct {
	ContainerID       string            `json:"containerId"`
	Enabled           *bool             `json:"enabled,omitempty"`
	Model             *string           `json:"model,omitempty"`
	Topics            *[]audience.Topic `json:"topics,omitempty"`
	SeverityThreshold *int              `json:"severityThreshold,omitempty"`
	Instructions      *string           `json:"instructions,omitempty"`
	UpdatedAt         time.Time         `json:"updatedAt"`
}

type ContainerSettingsResponse struct {
	Override  *ContainerOverrideResponse `json:"override,omitempty"`
	Effective SettingsResponse           `json:"effective"`
}

func toContainerSettingsResponse(cs *audience.ContainerSettings) ContainerSettingsResponse {
	out := ContainerSettingsResponse{Effective: toSettingsResponse(&cs.Effective)}
	if o := cs.Override; o != nil {
		r := &ContainerOverrideResponse{
			ContainerID: o.ContainerID, Enabled: o.Enabled, Model: o.Model,
			SeverityThreshold: o.SeverityThreshold, Instructions: o.Instructions, UpdatedAt: o.UpdatedAt,
		}
		if o.Topics != nil {
			t := []audience.Topic(*o.Topics)
			r.Topics = &t
		}
		out.Override = r
	}
	return out
}

type AuthorContainerResponse struct {
	Source      string `json:"source"`
	AccountID   string `json:"accountId"`
	ContainerID string `json:"containerId"`

	Comments          int `json:"comments"`
	StanceSupporter   int `json:"stanceSupporter"`
	StanceNeutral     int `json:"stanceNeutral"`
	StanceCritic      int `json:"stanceCritic"`
	StanceHostile     int `json:"stanceHostile"`
	SeverityMax       int `json:"severityMax"`
	SeverityHighCount int `json:"severityHighCount"`

	FirstOccurredAt time.Time `json:"firstOccurredAt"`
	LastOccurredAt  time.Time `json:"lastOccurredAt"`

	DerivedStance string `json:"derivedStance"`
	Reputation    int    `json:"reputation"`
}

func toAuthorContainerResponse(c *audience.AuthorContainer) AuthorContainerResponse {
	return AuthorContainerResponse{
		Source: string(c.Source), AccountID: c.AccountID, ContainerID: c.ContainerID,
		Comments:        c.Comments,
		StanceSupporter: c.Stances.Supporter, StanceNeutral: c.Stances.Neutral,
		StanceCritic: c.Stances.Critic, StanceHostile: c.Stances.Hostile,
		SeverityMax: c.SeverityMax, SeverityHighCount: c.SeverityHighCount,
		FirstOccurredAt: c.FirstOccurredAt, LastOccurredAt: c.LastOccurredAt,
		DerivedStance: string(c.DerivedStance), Reputation: c.Reputation,
	}
}

type AuthorContainersResponse struct {
	Author     AuthorResponse            `json:"author"`
	Containers []AuthorContainerResponse `json:"containers"`
	Page       int                       `json:"page"`
	PageSize   int                       `json:"pageSize"`
	Total      int64                     `json:"total"`
}
