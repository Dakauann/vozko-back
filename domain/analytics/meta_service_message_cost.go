package analytics

import (
	"strings"
	"time"

	"vozko/domain/shared"
	businessphone "vozko/domain/whatsapp/business_phone"
)

type ServiceMessageProvider string

const (
	ServiceMessageProviderAll          ServiceMessageProvider = ""
	ServiceMessageProviderMeta         ServiceMessageProvider = ServiceMessageProvider(businessphone.ProviderMeta)
	ServiceMessageProviderDialog360    ServiceMessageProvider = ServiceMessageProvider(businessphone.ProviderDialog360)
	ServiceMessageProviderUnattributed ServiceMessageProvider = "unattributed"
)

func (p ServiceMessageProvider) Normalized() ServiceMessageProvider {
	switch p {
	case ServiceMessageProviderAll,
		ServiceMessageProviderMeta,
		ServiceMessageProviderDialog360,
		ServiceMessageProviderUnattributed:
		return p
	default:
		return ServiceMessageProviderMeta
	}
}

func NormalizeServiceMessageProvider(raw string) ServiceMessageProvider {
	switch strings.ToLower(strings.TrimSpace(raw)) {
	case "all", "any", "*":
		return ServiceMessageProviderAll
	case "dialog360", "360dialog", "360":
		return ServiceMessageProviderDialog360
	case "unattributed", "none", "no_phone":
		return ServiceMessageProviderUnattributed
	default:
		return ServiceMessageProviderMeta
	}
}

type MetaServiceMessageCostSortField string

const (
	SortMetaServiceMessageCostRatio            MetaServiceMessageCostSortField = "ratio"
	SortMetaServiceMessageCostServiceMessages  MetaServiceMessageCostSortField = "serviceMessages"
	SortMetaServiceMessageCostNetBillableSends MetaServiceMessageCostSortField = "netBillableSends"
	SortMetaServiceMessageCostWorkspaceName    MetaServiceMessageCostSortField = "workspaceName"
)

func NormalizeMetaServiceMessageCostSortField(raw string) MetaServiceMessageCostSortField {
	switch strings.ToLower(strings.TrimSpace(raw)) {
	case "servicemessages", "service_messages", "messages", "svc":
		return SortMetaServiceMessageCostServiceMessages
	case "netbillablesends", "net_billable_sends", "sends", "net_sends":
		return SortMetaServiceMessageCostNetBillableSends
	case "workspacename", "workspace_name", "workspace", "name":
		return SortMetaServiceMessageCostWorkspaceName
	default:
		return SortMetaServiceMessageCostRatio
	}
}

type WorkspaceMetaServiceMessageCost struct {
	WorkspaceID   string `json:"workspaceId"`
	WorkspaceName string `json:"workspaceName"`

	Providers []string `json:"providers"`

	ServiceMessages int64 `json:"serviceMessages"`

	NetBillableSends int64 `json:"netBillableSends"`

	MetaConfirmed int64 `json:"metaConfirmed"`

	Ratio *float64 `json:"ratio,omitempty"`
}

func (t MetaServiceMessageCostTotals) FullyAnsweredByMeta() bool {
	return t.ServiceMessages > 0 && t.MetaAnswered >= t.ServiceMessages
}

func ComputeRatio(serviceMessages, netBillableSends int64) *float64 {
	if netBillableSends <= 0 {
		return nil
	}
	ratio := float64(serviceMessages) / float64(netBillableSends)
	return &ratio
}

type MetaServiceMessageCostTotals struct {
	ServiceMessages   int64    `json:"serviceMessages"`
	NetBillableSends  int64    `json:"netBillableSends"`
	Ratio             *float64 `json:"ratio,omitempty"`
	WorkspacesCovered int64    `json:"workspacesCovered"`

	MetaConfirmed int64 `json:"metaConfirmed"`

	MetaAnswered int64 `json:"metaAnswered"`

	UnattributedServiceMessages int64 `json:"unattributedServiceMessages"`
}

type MetaServiceMessageCostReport struct {
	Period   Period                       `json:"period"`
	Provider ServiceMessageProvider       `json:"provider"`
	Totals   MetaServiceMessageCostTotals `json:"totals"`

	Workspaces *shared.PaginatedResult[*WorkspaceMetaServiceMessageCost] `json:"workspaces"`

	InferredOnly bool `json:"inferredOnly"`
}

type MetaServiceMessageCostInput struct {
	StartDate time.Time
	EndDate   time.Time
	Provider  ServiceMessageProvider
	Search    string
	Page      int
	PageSize  int
	SortBy    MetaServiceMessageCostSortField
	SortOrder shared.SortDirection
}

type GetMetaServiceMessageCostUseCase interface {
	Execute(input MetaServiceMessageCostInput) (*MetaServiceMessageCostReport, error)
}
