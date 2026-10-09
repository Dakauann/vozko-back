package leadaction

import (
	"strings"
	"time"

	"vozko/domain/advertising"
)

const (
	AudienceStaleAfter = 30 * time.Minute
	AudienceRetention  = 24 * time.Hour
)

type AudienceStatus string

const (
	AudiencePending AudienceStatus = "pending"
	AudienceDone    AudienceStatus = "done"
	AudienceFailed  AudienceStatus = "failed"
)

type AudienceJob struct {
	ID          string                `json:"id"`
	WorkspaceID string                `json:"workspaceId"`
	ActorID     string                `json:"actorId"`
	Fingerprint string                `json:"fingerprint"`
	Status      AudienceStatus        `json:"status"`
	Audience    *advertising.Audience `json:"audience,omitempty"`
	Matched     int                   `json:"matched"`
	Skipped     int                   `json:"skipped"`
	FailureCode string                `json:"failureCode,omitempty"`
	StartedAt   time.Time             `json:"startedAt"`
	UpdatedAt   time.Time             `json:"updatedAt"`
}

func (j AudienceJob) Effective(now time.Time) AudienceJob {
	if j.Status == AudiencePending && now.Sub(j.UpdatedAt) > AudienceStaleAfter {
		j.Status, j.FailureCode = AudienceFailed, string(FailureStalled)
	}
	return j
}

func (j AudienceJob) VisibleTo(userID string) bool {
	return strings.TrimSpace(userID) != "" && userID == j.ActorID
}

func (j *AudienceJob) Succeed(a advertising.Audience, matched, skipped int, now time.Time) {
	j.Status, j.Audience, j.Matched, j.Skipped, j.FailureCode, j.UpdatedAt = AudienceDone, &a, matched, skipped, "", now.UTC()
}

func (j *AudienceJob) Fail(code string, now time.Time) {
	if strings.TrimSpace(code) == "" {
		code = string(FailureInternal)
	}
	j.Status, j.FailureCode, j.UpdatedAt = AudienceFailed, code, now.UTC()
}
