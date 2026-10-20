package whatsappform

import (
	"slices"
	"strconv"
	"strings"
	"time"
)

type PublicationStatus string

const (
	PublicationCreating   PublicationStatus = "creating"
	PublicationDraft      PublicationStatus = "draft"
	PublicationPublished  PublicationStatus = "published"
	PublicationThrottled  PublicationStatus = "throttled"
	PublicationBlocked    PublicationStatus = "blocked"
	PublicationDeprecated PublicationStatus = "deprecated"
	PublicationFailed     PublicationStatus = "failed"
)

var publicationMoves = map[PublicationStatus][]PublicationStatus{
	PublicationCreating:  {PublicationDraft, PublicationPublished, PublicationFailed},
	PublicationDraft:     {PublicationPublished, PublicationFailed, PublicationCreating},
	PublicationPublished: {PublicationThrottled, PublicationBlocked, PublicationDeprecated},
	PublicationThrottled: {PublicationPublished, PublicationBlocked, PublicationDeprecated},
	PublicationBlocked:   {PublicationThrottled, PublicationDeprecated},
	PublicationFailed:    {PublicationCreating, PublicationDraft, PublicationPublished},
}

func (s PublicationStatus) CanMoveTo(next PublicationStatus) bool {
	return s == next || slices.Contains(publicationMoves[s], next)
}

func (s PublicationStatus) Sendable() bool {
	return s == PublicationPublished || s == PublicationThrottled
}

var metaStatuses = map[string]PublicationStatus{
	"DRAFT":      PublicationDraft,
	"PUBLISHED":  PublicationPublished,
	"THROTTLED":  PublicationThrottled,
	"BLOCKED":    PublicationBlocked,
	"DEPRECATED": PublicationDeprecated,
}

func PublicationStatusFromMeta(status string) PublicationStatus {
	return metaStatuses[status]
}

type Publication struct {
	ID               string            `json:"id"`
	WorkspaceID      string            `json:"workspaceId"`
	FormID           string            `json:"formId"`
	WABAID           string            `json:"wabaId"`
	MetaWABAID       string            `json:"metaWabaId"`
	MetaFlowID       string            `json:"metaFlowId,omitempty"`
	MetaName         string            `json:"metaName"`
	VersionID        string            `json:"versionId"`
	Status           PublicationStatus `json:"status"`
	Problems         Problems          `json:"problems,omitempty"`
	LastError        string            `json:"lastError,omitempty"`
	PreviewURL       string            `json:"previewUrl,omitempty"`
	PreviewExpiresAt *time.Time        `json:"previewExpiresAt,omitempty"`
	PublishedAt      *time.Time        `json:"publishedAt,omitempty"`
	CreatedAt        time.Time         `json:"createdAt"`
	UpdatedAt        time.Time         `json:"updatedAt"`
}

func (p Publication) Sendable() bool {
	return p.MetaFlowID != "" && p.Status.Sendable()
}

func MetaFlowName(formName, formID string) string {
	name := []rune(formName)
	if len(name) > 60 {
		name = name[:60]
	}
	return strings.TrimSpace(string(name)) + " · Vozko " + shortID(formID)
}

func shortID(id string) string {
	if len(id) > 8 {
		return id[:8]
	}
	return id
}

func PreviewFlowName(formID string) string {
	return "Vozko · prévia " + shortID(formID)
}

func RepublishedFlowName(formName, formID string, versionNumber int) string {
	return MetaFlowName(formName, formID) + " · v" + strconv.Itoa(versionNumber)
}
