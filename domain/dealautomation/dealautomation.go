package dealautomation

import (
	"errors"
	"time"

	"vozko/domain/conversation"
	"vozko/domain/shared"
	"vozko/domain/workspace"
)

var ErrUnsupportedChannel = errors.New("deal automation: this channel does not support automatic deals")

type Channel struct {
	EntryType shared.EntryType
	Kind      conversation.ContainerKind
}

var channelResources = map[Channel]workspace.Resource{
	{EntryType: shared.EntryTypeWhatsApp, Kind: conversation.ContainerKindCampaign}:           workspace.ResourceWhatsAppCampaigns,
	{EntryType: shared.EntryTypeUnofficialWhatsApp, Kind: conversation.ContainerKindAccount}:  workspace.ResourceUnofficialWhatsAppInstances,
	{EntryType: shared.EntryTypeUnofficialWhatsApp, Kind: conversation.ContainerKindCampaign}: workspace.ResourceUnofficialWhatsAppCampaigns,
	{EntryType: shared.EntryTypeInstagram, Kind: conversation.ContainerKindAccount}:           workspace.ResourceInstagramAccounts,
	{EntryType: shared.EntryTypeFacebook, Kind: conversation.ContainerKindAccount}:            workspace.ResourceFacebookPages,
	{EntryType: shared.EntryTypeTelegram, Kind: conversation.ContainerKindAccount}:            workspace.ResourceTelegramAccounts,
	{EntryType: shared.EntryTypeWebchat, Kind: conversation.ContainerKindAccount}:             workspace.ResourceWebchatWidgets,
}

func (c Channel) Resource() (workspace.Resource, bool) {
	resource, ok := channelResources[c]
	return resource, ok
}

type Setting struct {
	WorkspaceID string    `json:"workspaceId"`
	Channel     Channel   `json:"-"`
	ContainerID string    `json:"containerId"`
	PipelineID  string    `json:"pipelineId"`
	UpdatedBy   string    `json:"updatedBy,omitempty"`
	UpdatedAt   time.Time `json:"updatedAt"`
}

func (s Setting) Enabled() bool {
	return s.PipelineID != ""
}

type Repository interface {
	Find(workspaceID string, channel Channel, containerID string) (*Setting, error)
	Save(setting Setting) error
	Delete(workspaceID string, channel Channel, containerID string) error
}
