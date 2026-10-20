package whatsappform

import (
	"context"
	"errors"
	"fmt"
	"time"

	"vozko/domain/workspace"
)

const (
	CapabilityView               workspace.CapabilityKey = "whatsapp_forms.view"
	CapabilityBuild              workspace.CapabilityKey = "whatsapp_forms.build"
	CapabilityPublish            workspace.CapabilityKey = "whatsapp_forms.publish"
	CapabilitySend               workspace.CapabilityKey = "whatsapp_forms.send"
	CapabilityDelete             workspace.CapabilityKey = "whatsapp_forms.delete"
	CapabilityResponses          workspace.CapabilityKey = "whatsapp_forms.responses"
	CapabilityResponsesSensitive workspace.CapabilityKey = "whatsapp_forms.responses_sensitive"
	CapabilityExport             workspace.CapabilityKey = "whatsapp_forms.export"
	CapabilityResponsesDelete    workspace.CapabilityKey = "whatsapp_forms.responses_delete"
)

type PublicationBrief struct {
	MetaWABAID string            `json:"metaWabaId"`
	Status     PublicationStatus `json:"status"`
	VersionID  string            `json:"versionId"`
}

type FormSummary struct {
	Form           Form               `json:"form"`
	Publications   []PublicationBrief `json:"publications"`
	Sent           int64              `json:"sent"`
	Responses      int64              `json:"responses"`
	LastResponseAt *time.Time         `json:"lastResponseAt,omitempty"`
}

type FormQuery struct {
	WorkspaceID     string
	Search          string
	IncludeArchived bool
}

type FormRepository interface {
	Create(ctx context.Context, form Form, version Version) error
	Get(ctx context.Context, workspaceID, formID string) (*Form, error)
	List(ctx context.Context, q FormQuery) ([]FormSummary, error)
	SaveDraft(ctx context.Context, workspaceID, formID, baseVersionID string, version Version, at time.Time) error
	Rename(ctx context.Context, workspaceID, formID, name, description string, at time.Time) error
	SetPublished(ctx context.Context, workspaceID, formID, versionID string, at time.Time) error
	Archive(ctx context.Context, workspaceID, formID string, at time.Time) error
	PublishedWorkspaces(ctx context.Context) ([]string, error)
}

type VersionRepository interface {
	Get(ctx context.Context, workspaceID, versionID string) (*Version, error)
	List(ctx context.Context, workspaceID, formID string) ([]Version, error)
}

type PublicationRepository interface {
	ListForForm(ctx context.Context, workspaceID, formID string) ([]Publication, error)
	Find(ctx context.Context, workspaceID, formID, metaWABAID string) (*Publication, error)
	FindByMetaFlow(ctx context.Context, metaWABAID, metaFlowID string) (*Publication, error)
	Insert(ctx context.Context, p Publication) error
	Save(ctx context.Context, p Publication) error
	ListStuck(ctx context.Context, before time.Time, limit int) ([]Publication, error)
}

type SendQuery struct {
	WorkspaceID string
	FormID      string
	After       string
	Limit       int
	Scope       ReadScope
}

type SendRepository interface {
	Insert(ctx context.Context, s Send) error
	Update(ctx context.Context, s Send) error
	FindByToken(ctx context.Context, token string) (*Send, error)
	FindByMessageID(ctx context.Context, messageID string) (*Send, error)
	List(ctx context.Context, q SendQuery) (Page[Send], error)
}

type StatsQuery struct {
	WorkspaceID string
	FormID      string
	From, To    *time.Time
	Scope       ReadScope
}

type OriginStat struct {
	Origin    SendOrigin `json:"origin"`
	Sent      int64      `json:"sent"`
	Responses int64      `json:"responses"`
}

type DayStat struct {
	Day       string `json:"day"`
	Sent      int64  `json:"sent"`
	Responses int64  `json:"responses"`
}

type Stats struct {
	Sends                  map[SendStatus]int64 `json:"sends"`
	Delivered              int64                `json:"delivered"`
	Read                   int64                `json:"read"`
	Responses              int64                `json:"responses"`
	MedianSecondsToRespond *float64             `json:"medianSecondsToRespond,omitempty"`
	ByOrigin               []OriginStat         `json:"byOrigin"`
	ByDay                  []DayStat            `json:"byDay"`
}

type ResponseRepository interface {
	Insert(ctx context.Context, r Response) (Response, bool, error)
	SaveApply(ctx context.Context, workspaceID, responseID string, status ApplyStatus, report ApplyReport) error
	Get(ctx context.Context, workspaceID, responseID string, scope ReadScope) (*Response, error)
	List(ctx context.Context, q ResponseQuery) (Page[Response], error)
	Delete(ctx context.Context, workspaceID, responseID string) error
	Stats(ctx context.Context, q StatsQuery) (Stats, error)
	Distribution(ctx context.Context, q StatsQuery, fieldKey string) (map[string]int64, error)
}

type FlowAccount struct {
	MetaWABAID  string
	AccessToken string
}

type RemoteFlow struct {
	ID               string
	Name             string
	Status           string
	ValidationErrors []MetaValidationError
	PreviewURL       string
	PreviewExpiresAt *time.Time
}

type CreateFlowInput struct {
	Name       string
	Categories []Category
	JSON       []byte
	Publish    bool
}

type FlowsGateway interface {
	Create(ctx context.Context, account FlowAccount, in CreateFlowInput) (RemoteFlow, error)
	UploadJSON(ctx context.Context, account FlowAccount, flowID string, json []byte) ([]MetaValidationError, error)
	Publish(ctx context.Context, account FlowAccount, flowID string) error
	Deprecate(ctx context.Context, account FlowAccount, flowID string) error
	Get(ctx context.Context, account FlowAccount, flowID string) (RemoteFlow, error)
	FindByName(ctx context.Context, account FlowAccount, name string) (*RemoteFlow, error)
	Preview(ctx context.Context, account FlowAccount, flowID string, refresh bool) (RemoteFlow, error)
	List(ctx context.Context, account FlowAccount) ([]RemoteFlow, error)
	DownloadJSON(ctx context.Context, account FlowAccount, flowID string) ([]byte, error)
}

var ErrMetaRejected = errors.New("whatsappform: Meta refused the form")

type MetaRejection struct {
	Code     int
	Message  string
	Problems []MetaValidationError
}

func (r *MetaRejection) Error() string {
	return fmt.Sprintf("%s: code %d: %s", ErrMetaRejected, r.Code, r.Message)
}

func (r *MetaRejection) Unwrap() error { return ErrMetaRejected }
