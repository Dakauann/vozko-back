package whatsapp_campaign

import (
	"context"

	"vozko/domain/campaign"
	"vozko/domain/shared"

	wce "vozko/domain/whatsapp_campaign_entry"
	wd "vozko/domain/workspace/workspace_department"
)

type CreateCampaignUseCase interface {
	Execute(ctx context.Context, campaign *Campaign) (*Campaign, error)
}

type UpdateCampaignUseCase interface {
	Execute(campaignID string, campaign *Campaign) (*Campaign, error)
}

type AssignDepartmentUseCase interface {
	Execute(ctx context.Context, campaignID string) (*Campaign, error)
}

type DeleteCampaignUseCase interface {
	Execute(campaignID string) error
}

type GetCampaignUseCase interface {
	Execute(campaignID string) (*Campaign, error)
}

type ListCampaignsUseCase interface {
	Execute(input ListCampaignsInput) (*shared.PaginatedResult[*Campaign], error)
}

type GetSummaryUseCase interface {
	Execute(filter wce.WorkspaceSummaryFilter) (*CampaignMetrics, error)
}

type EnsureOrganicCoexistenceCampaignUseCase interface {
	Execute(workspaceID, businessPhoneID, displayPhoneNumber string) (*Campaign, bool, error)
}

type ListEntriesUseCase interface {
	Execute(input wce.ListEntriesInput) (*shared.PaginatedResult[*wce.EntryWithLead], error)
}

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
	ConfirmReset(input ResetCampaignInput) (*ResetCampaignOutput, error)
}

type CampaignAction = campaign.Action

const (
	CampaignActionStart = campaign.ActionStart
	CampaignActionPause = campaign.ActionPause
	CampaignActionStop  = campaign.ActionStop
)

const (
	Exchange = "whatsapp_campaign_exchange"

	WhatsAppCampaignDispatchTopic = "whatsapp_campaign_dispatch"
)

var QueueNamespace = campaign.Namespace{
	Topic: WhatsAppCampaignDispatchTopic,
	Key:   "campaign:whatsapp",
}

type DispatchEntry struct {
	EntryID     string
	PhoneNumber string
}

type DispatchCampaignInput struct {
	CampaignID string
	Entries    []DispatchEntry
	Action     CampaignAction
}

type DispatchQueueMessage struct {
	CampaignID  string `json:"campaignId"`
	EntryID     string `json:"entryId"`
	PhoneNumber string `json:"phoneNumber,omitempty"`
}

type DispatchCampaignUseCase interface {
	Dispatch(input DispatchCampaignInput) error
}

type MessageConsumerUseCase interface {
	Start() error
	SubscribeToCampaign(campaignID string) error
	StopCampaignConsumer(campaignID string) error
	PauseCampaignConsumer(campaignID string) error
	ResumeCampaignConsumer(campaignID string) error
	IsSubscribed(campaignID string) bool
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
	ConfirmClearHistory(input ClearHistoryInput) (*ClearHistoryOutput, error)
}

type EntryInput struct {
	Number    string                 `json:"number"`
	Name      string                 `json:"name,omitempty"`
	Variables []string               `json:"variables,omitempty"`
	Metadata  map[string]interface{} `json:"metadata,omitempty"`
}

type EntryOutput struct {
	EntryID           string                 `json:"entryId"`
	CampaignID        string                 `json:"campaignId"`
	LeadID            string                 `json:"leadId"`
	Number            string                 `json:"number"`
	Name              string                 `json:"name,omitempty"`
	Variables         []string               `json:"variables,omitempty"`
	Status            string                 `json:"status"`
	AutomationEnabled *bool                  `json:"automationEnabled,omitempty"`
	Metadata          map[string]interface{} `json:"metadata,omitempty"`
	CreatedAt         string                 `json:"createdAt"`
	UpdatedAt         string                 `json:"updatedAt"`
}

type DeleteEntryInput struct {
	CampaignID string
	EntryID    string
}

type DeleteEntryUseCase interface {
	Execute(input DeleteEntryInput) error
}

type UpdateEntryInput struct {
	CampaignID        string
	EntryID           string
	Number            *string                `json:"number,omitempty"`
	Name              *string                `json:"name,omitempty"`
	Variables         []string               `json:"variables,omitempty"`
	Metadata          map[string]interface{} `json:"metadata,omitempty"`
	AutomationEnabled *bool                  `json:"automationEnabled,omitempty"`
}

type UpdateEntryUseCase interface {
	Execute(input UpdateEntryInput) (*EntryOutput, error)
}

type AddEntriesInput struct {
	CampaignID   string       `json:"campaignId"`
	PhoneNumbers []EntryInput `json:"phoneNumbers"`
}

type AddEntriesOutput struct {
	AddedCount        int           `json:"addedCount"`
	DuplicatesSkipped int           `json:"duplicatesSkipped"`
	Entries           []EntryOutput `json:"entries"`
}

type AddEntriesUseCase interface {
	Execute(input AddEntriesInput) (*AddEntriesOutput, error)
}

type QuickSendInput struct {
	CampaignID   string       `json:"campaignId"`
	PhoneNumbers []EntryInput `json:"phoneNumbers,omitempty"`
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
	Execute(input QuickSendInput) (*QuickSendOutput, error)
}

type CampaignAccessUseCase interface {
	Owned(workspaceID string, departments *wd.DepartmentFilter, campaignID string) (*Campaign, error)
}

type StartCampaignUseCase interface {
	Start(workspaceID string, departments *wd.DepartmentFilter, campaignID string) (*Campaign, error)
}
