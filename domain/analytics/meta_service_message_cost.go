package analytics

import (
	"strings"
	"time"

	"vozko/domain/shared"
	businessphone "vozko/domain/whatsapp/business_phone"
)

// From 1 October 2026 Meta charges for service messages, the free-form replies
// an agent or our AI sends inside the 24 hour window. We absorb that cost and
// raise the price per campaign trigger instead, which only works if we can see,
// per workspace, how much messaging each one does against how much billable
// campaign volume it buys.
//
// This report is that comparison and nothing else. It deliberately reports
// counts rather than money: Meta prices per recipient market, so a count only
// becomes currency once a rate card and an exchange rate are attached, and
// neither is ours to promise.
//
// The two sides come from two different places, which is the whole shape of it:
// service messages are counted from conversation_messages, because nothing in
// the tree ever writes a balance transaction for them, while billable campaign
// sends are read from the ledger where the money actually moved.

// ServiceMessageProvider narrows the report to numbers hosted through one
// WhatsApp Business Solution Provider.
//
// It matters because it decides whose bill this is. Numbers we fund are our
// exposure; numbers funded elsewhere are not. Keeping it a filter rather than a
// constant is what lets the report survive a migration between providers.
type ServiceMessageProvider string

const (
	// ServiceMessageProviderAll counts every official number, including those
	// whose campaign carries no business phone at all.
	ServiceMessageProviderAll ServiceMessageProvider = ""
	// ServiceMessageProviderMeta is a number onboarded directly against Meta.
	// This is the default, because it is where our cost sits today.
	//
	// Converted from businessphone.Provider rather than written out, because
	// these values are compared against the provider column that entity fills.
	// Spelling them again here would be a second copy of the same vocabulary,
	// and the day one of them changed this report would filter on a value the
	// column no longer holds and quietly return zero.
	ServiceMessageProviderMeta ServiceMessageProvider = ServiceMessageProvider(businessphone.ProviderMeta)
	// ServiceMessageProviderDialog360 is a number onboarded through 360dialog.
	ServiceMessageProviderDialog360 ServiceMessageProvider = ServiceMessageProvider(businessphone.ProviderDialog360)
	// ServiceMessageProviderUnattributed selects messages whose campaign has no
	// business phone, so no provider can be named for them.
	//
	// It exists so those messages can be looked at rather than silently
	// disappearing behind a provider filter. A report that quietly loses volume
	// is worse than no report, because nobody can tell it happened.
	ServiceMessageProviderUnattributed ServiceMessageProvider = "unattributed"
)

// Normalized returns the provider when it is one this report knows, and Meta
// otherwise.
//
// It cannot be written in terms of NormalizeServiceMessageProvider, because the
// empty string means two different things in the two directions: an absent
// query parameter should default to Meta, while an already-decided
// ServiceMessageProviderAll must be preserved.
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

// NormalizeServiceMessageProvider maps free text from a query string onto a
// known provider, falling back to Meta. The fallback is deliberate: an
// unrecognised filter must not silently widen the report to numbers somebody
// else pays for.
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

// MetaServiceMessageCostSortField is the whitelist of orderings the report accepts.
// It lives in the domain because the usecase validates against it and the
// repository maps it to a column, and those two must not disagree.
type MetaServiceMessageCostSortField string

const (
	SortMetaServiceMessageCostRatio            MetaServiceMessageCostSortField = "ratio"
	SortMetaServiceMessageCostServiceMessages  MetaServiceMessageCostSortField = "serviceMessages"
	SortMetaServiceMessageCostNetBillableSends MetaServiceMessageCostSortField = "netBillableSends"
	SortMetaServiceMessageCostWorkspaceName    MetaServiceMessageCostSortField = "workspaceName"
)

// NormalizeMetaServiceMessageCostSortField falls back to ratio, which is the question
// the page exists to answer: who sends more than they buy.
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

// WorkspaceMetaServiceMessageCost is one workspace's side of the comparison over the
// reported period.
type WorkspaceMetaServiceMessageCost struct {
	WorkspaceID   string `json:"workspaceId"`
	WorkspaceName string `json:"workspaceName"`

	// Providers are the distinct providers that produced the counted messages,
	// sorted. A workspace can hold numbers on more than one, and collapsing that
	// to a single value would misreport who is paying.
	Providers []string `json:"providers"`

	// ServiceMessages is what Meta will charge us for: outbound, non-template,
	// delivered messages on official numbers matching the provider filter.
	ServiceMessages int64 `json:"serviceMessages"`

	// NetBillableSends is template sends the workspace actually paid for,
	// debits minus refunds. Gross debits overstate this by about a fifth.
	NetBillableSends int64 `json:"netBillableSends"`

	// MetaConfirmed is the subset of ServiceMessages that Meta itself stamped
	// billable, rather than our own reading of the message log.
	//
	// It stays at zero for every message delivered before the pricing columns
	// existed, so it climbs toward ServiceMessages rather than starting there.
	// The gap between the two is what the free entry point exemption and our
	// own history account for.
	MetaConfirmed int64 `json:"metaConfirmed"`

	// Ratio is ServiceMessages per NetBillableSends, nil when there were no
	// billable sends at all.
	//
	// Nil is not zero and must never be rendered as one. A workspace that sends
	// service messages and buys nothing is the worst case on this page, so it
	// sorts above every finite ratio rather than below them.
	Ratio *float64 `json:"ratio,omitempty"`
}

// FullyAnsweredByMeta reports whether Meta has given a verdict on every message
// the period counted, which is what stops the report being an inference.
//
// A period with nothing in it is not "fully answered": there is no confirmation
// to have, so the report stays marked as our own reading rather than claiming a
// certainty it never obtained.
func (t MetaServiceMessageCostTotals) FullyAnsweredByMeta() bool {
	return t.ServiceMessages > 0 && t.MetaAnswered >= t.ServiceMessages
}

// ComputeRatio is the one definition of the ratio, used by the repository when
// it builds a row so the number the page sorts by and the number it prints can
// never disagree.
func ComputeRatio(serviceMessages, netBillableSends int64) *float64 {
	if netBillableSends <= 0 {
		return nil
	}
	ratio := float64(serviceMessages) / float64(netBillableSends)
	return &ratio
}

// MetaServiceMessageCostTotals are the period totals, across every workspace matching
// the filters and not only the page being shown.
type MetaServiceMessageCostTotals struct {
	ServiceMessages   int64    `json:"serviceMessages"`
	NetBillableSends  int64    `json:"netBillableSends"`
	Ratio             *float64 `json:"ratio,omitempty"`
	WorkspacesCovered int64    `json:"workspacesCovered"`

	// MetaConfirmed is how much of ServiceMessages Meta has stamped billable
	// itself. It is the measure of how far the report still is from Meta's
	// truth, which is why it is reported next to the total rather than instead
	// of it.
	MetaConfirmed int64 `json:"metaConfirmed"`

	// MetaAnswered is how many of the counted messages Meta has said ANYTHING
	// about, billable or not.
	//
	// It is not MetaConfirmed. A message Meta told us was free inside the 72
	// hour entry point is answered but not confirmed, and the difference is
	// exactly the overcount our own inference cannot see. Coverage is what
	// decides whether this report is still an estimate, so it is measured
	// rather than assumed.
	MetaAnswered int64 `json:"metaAnswered"`

	// UnattributedServiceMessages are messages on campaigns with no business
	// phone, so no provider could be named. They are reported separately rather
	// than folded into the total, because they are the one bucket the provider
	// filter cannot speak for.
	UnattributedServiceMessages int64 `json:"unattributedServiceMessages"`
}

// MetaServiceMessageCostReport is the whole report.
type MetaServiceMessageCostReport struct {
	Period   Period                       `json:"period"`
	Provider ServiceMessageProvider       `json:"provider"`
	Totals   MetaServiceMessageCostTotals `json:"totals"`

	Workspaces *shared.PaginatedResult[*WorkspaceMetaServiceMessageCost] `json:"workspaces"`

	// InferredOnly marks the counts as our own reading of the message log rather
	// than Meta's billing truth.
	//
	// While it is true the figure is an UPPER BOUND: messages inside the 72 hour
	// free entry point stay free at Meta but are indistinguishable to us until
	// the status webhook's pricing and origin fields are persisted. The page has
	// to say so, or somebody will negotiate against a number that is too high.
	//
	// It is DERIVED from coverage, never asserted. Every message in the period
	// is counted from our own rule until Meta has answered for all of them, and
	// then it stops being an inference on its own. Hardcoding it would mean the
	// caveat outlived the reason for it and the page kept apologising for a
	// number that had become exact.
	InferredOnly bool `json:"inferredOnly"`
}

// MetaServiceMessageCostInput is the request, before normalization.
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
