package unofficial_whatsapp_campaign

import (
	"context"

	"vozko/domain/campaign"
	"vozko/domain/shared"
	uw "vozko/domain/unofficial_whatsapp"
)

// The use-case ports, mirroring domain/whatsapp_campaign/usecases.go one for
// one so the two channels' handlers, wiring and screens have the same shape —
// plus the two this channel needs and the official one has no reason to.

type CreateCampaignUseCase interface {
	Execute(ctx context.Context, in *Campaign, scope uw.DepartmentScope) (*Campaign, error)
}

type UpdateCampaignUseCase interface {
	Execute(ctx context.Context, campaignID string, in *Campaign, scope uw.DepartmentScope) (*Campaign, error)
}

type AssignDepartmentUseCase interface {
	Execute(ctx context.Context, campaignID string) (*Campaign, error)
}

type DeleteCampaignUseCase interface {
	Execute(campaignID string) error
}

type GetCampaignUseCase interface {
	Execute(ctx context.Context, campaignID string) (*Campaign, error)
}

type ListCampaignsUseCase interface {
	Execute(ctx context.Context, in ListCampaignsInput) (*shared.PaginatedResult[*Campaign], error)
}

type GetSummaryUseCase interface {
	Execute(filter WorkspaceSummaryFilter) (*campaign.Metrics, error)
}

type ListEntriesUseCase interface {
	Execute(in ListEntriesInput) (*shared.PaginatedResult[*EntryWithLead], error)
}

// ---------------------------------------------------------------- lifecycle

type DispatchEntry struct {
	EntryID     string
	PhoneNumber string
}

type DispatchCampaignInput struct {
	CampaignID string
	Entries    []DispatchEntry
	Action     campaign.Action
}

type DispatchCampaignUseCase interface {
	Dispatch(ctx context.Context, in DispatchCampaignInput) error
}

type MessageConsumerUseCase interface {
	Start() error
	SubscribeToCampaign(campaignID string) error
	StopCampaignConsumer(campaignID string) error
	PauseCampaignConsumer(campaignID string) error
	ResumeCampaignConsumer(campaignID string) error
	IsSubscribed(campaignID string) bool
}

type StartScheduleJob interface {
	StartScheduledCampaigns() error
}

// ---------------------------------------------------------------- reset / clear

type ResetCampaignInput struct {
	CampaignID string
	ResetCode  string
}

type ResetCampaignOutput struct {
	Campaign     *Campaign `json:"campaign"`
	ResetCount   int64     `json:"resetCount"`
	NewResetCode string    `json:"newResetCode"`
}

type PrepareResetOutput struct {
	CampaignID string `json:"campaignId"`
	ResetCode  string `json:"resetCode"`
	Message    string `json:"message"`
}

type ResetCampaignUseCase interface {
	PrepareReset(campaignID string) (*PrepareResetOutput, error)
	ConfirmReset(in ResetCampaignInput) (*ResetCampaignOutput, error)
}

type ClearHistoryInput struct {
	CampaignID string
	ClearCode  string
}

type ClearHistoryOutput struct {
	Campaign     *Campaign `json:"campaign"`
	DeletedCount int64     `json:"deletedCount"`
	NewClearCode string    `json:"newClearCode,omitempty"`
}

type PrepareClearHistoryOutput struct {
	CampaignID   string `json:"campaignId"`
	ClearCode    string `json:"clearCode"`
	MessageCount int64  `json:"messageCount"`
	Message      string `json:"message"`
}

type ClearHistoryUseCase interface {
	PrepareClearHistory(campaignID string) (*PrepareClearHistoryOutput, error)
	ConfirmClearHistory(in ClearHistoryInput) (*ClearHistoryOutput, error)
}

// ---------------------------------------------------------------- entries

type EntryInput struct {
	Number    string                 `json:"number"`
	Name      string                 `json:"name,omitempty"`
	Variables []string               `json:"variables,omitempty"`
	Metadata  map[string]interface{} `json:"metadata,omitempty"`
}

type EntryOutput struct {
	EntryID        string                 `json:"entryId"`
	CampaignID     string                 `json:"campaignId"`
	LeadID         string                 `json:"leadId"`
	Number         string                 `json:"number"`
	Name           string                 `json:"name,omitempty"`
	Variables      []string               `json:"variables,omitempty"`
	Status         string                 `json:"status"`
	ConversationID string                 `json:"conversationId,omitempty"`
	Metadata       map[string]interface{} `json:"metadata,omitempty"`
	CreatedAt      string                 `json:"createdAt"`
	UpdatedAt      string                 `json:"updatedAt"`
}

type AddEntriesInput struct {
	CampaignID string       `json:"campaignId"`
	Numbers    []EntryInput `json:"numbers"`
}

type AddEntriesOutput struct {
	AddedCount        int           `json:"addedCount"`
	DuplicatesSkipped int           `json:"duplicatesSkipped"`
	InvalidSkipped    int           `json:"invalidSkipped"`
	Entries           []EntryOutput `json:"entries"`
}

type AddEntriesUseCase interface {
	Execute(ctx context.Context, in AddEntriesInput) (*AddEntriesOutput, error)
}

type UpdateEntryInput struct {
	CampaignID string
	EntryID    string
	Number     *string                `json:"number,omitempty"`
	Name       *string                `json:"name,omitempty"`
	Variables  []string               `json:"variables,omitempty"`
	Metadata   map[string]interface{} `json:"metadata,omitempty"`
}

type UpdateEntryUseCase interface {
	Execute(ctx context.Context, in UpdateEntryInput) (*EntryOutput, error)
}

type DeleteEntryInput struct {
	CampaignID string
	EntryID    string
}

type DeleteEntryUseCase interface {
	Execute(in DeleteEntryInput) error
}

type QuickSendInput struct {
	CampaignID string       `json:"campaignId"`
	Numbers    []EntryInput `json:"numbers,omitempty"`
}

type QuickSendOutput struct {
	CampaignID        string        `json:"campaignId"`
	Status            string        `json:"status"`
	AddedCount        int           `json:"addedCount"`
	DuplicatesSkipped int           `json:"duplicatesSkipped"`
	DispatchedCount   int           `json:"dispatchedCount"`
	Entries           []EntryOutput `json:"entries"`
}

type QuickSendUseCase interface {
	Execute(ctx context.Context, in QuickSendInput) (*QuickSendOutput, error)
}

// ---------------------------------------------------------------- this channel only

// ValidateTargetsOutput reports what an up-front list clean found.
type ValidateTargetsOutput struct {
	CampaignID string `json:"campaignId"`
	Checked    int    `json:"checked"`
	OnWhatsApp int    `json:"onWhatsApp"`
	Skipped    int    `json:"skipped"`
}

// ValidateTargetsUseCase checks pending numbers against WhatsApp in batches and
// marks the dead ones SKIPPED_NOT_ON_WHATSAPP.
//
// Optional — the consumer checks per send anyway — but an operator cleaning a
// purchased list before committing to it is the difference between a blast that
// is 30% dead on arrival and one that is not. Sending to unregistered numbers is
// the loudest spam signal a linked device can emit.
type ValidateTargetsUseCase interface {
	Execute(ctx context.Context, campaignID string) (*ValidateTargetsOutput, error)
}

// PauseCampaignsForInstanceUseCase is the circuit breaker's actuator.
//
// Called when WhatsApp restricts a number, when the session drops, or when the
// health cron notices either. It pauses EVERY running campaign on that number,
// because the restriction belongs to the number and not to the campaign that
// happened to discover it. Resume is manual by design: somebody should look at
// the number before blasting from it again.
type PauseCampaignsForInstanceUseCase interface {
	Execute(ctx context.Context, instanceID, reason string) (paused int, err error)
}
