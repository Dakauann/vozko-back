package mercadolivre

import (
	"fmt"
	"regexp"
	"time"
)

const ThreadStatusFreshness = 6 * time.Hour

type PackItem struct {
	ItemID       string  `json:"itemId"`
	Title        string  `json:"title"`
	Quantity     int     `json:"quantity"`
	UnitPrice    float64 `json:"unitPrice"`
	CurrencyID   string  `json:"currencyId,omitempty"`
	ThumbnailKey string  `json:"thumbnailKey,omitempty"`
	Permalink    string  `json:"permalink,omitempty"`
}

type PackSummary struct {
	PackID            string     `json:"packId,omitempty"`
	OrderIDs          []string   `json:"orderIds"`
	Items             []PackItem `json:"items"`
	OrderStatus       string     `json:"orderStatus,omitempty"`
	ShipmentID        string     `json:"shipmentId,omitempty"`
	ShippingStatus    string     `json:"shippingStatus,omitempty"`
	ShippingSubstatus string     `json:"shippingSubstatus,omitempty"`
	TotalAmount       float64    `json:"totalAmount"`
	CurrencyID        string     `json:"currencyId,omitempty"`
}

type Conversation struct {
	ID          string `json:"id"`
	WorkspaceID string `json:"workspaceId"`
	AccountID   string `json:"accountId"`
	ContactID   string `json:"contactId"`

	PackKey       string   `json:"packKey"`
	OrderIDs      []string `json:"orderIds"`
	BuyerMLUserID string   `json:"buyerMlUserId,omitempty"`

	MLStatus       string     `json:"mlStatus"`
	MLSubstatus    string     `json:"mlSubstatus,omitempty"`
	MLStatusReadAt *time.Time `json:"mlStatusReadAt,omitempty"`

	AgentMediated        *bool `json:"agentMediated,omitempty"`
	OutboundBudget       int   `json:"outboundBudget"`
	OutboundSinceInbound int   `json:"outboundSinceInbound"`
	WindowOpen           bool  `json:"windowOpen"`

	PackSummary            *PackSummary `json:"packSummary,omitempty"`
	PackSummaryRefreshedAt *time.Time   `json:"packSummaryRefreshedAt,omitempty"`

	ConversationStatus string     `json:"conversationStatus,omitempty"`
	CloseSource        string     `json:"closeSource,omitempty"`
	CloseReason        string     `json:"closeReason,omitempty"`
	ClosedAt           *time.Time `json:"closedAt,omitempty"`
	AutomationEnabled  *bool      `json:"automationEnabled,omitempty"`

	LastMessageAt         *time.Time `json:"lastMessageAt,omitempty"`
	LastCustomerMessageAt *time.Time `json:"lastCustomerMessageAt,omitempty"`
	AwaitingReplySince    *time.Time `json:"awaitingReplySince,omitempty"`

	CreatedAt time.Time `json:"createdAt"`
	UpdatedAt time.Time `json:"updatedAt"`
}

func (c *Conversation) StatusIsStale(now time.Time) bool {
	if c.MLStatusReadAt == nil {
		return true
	}
	return now.Sub(*c.MLStatusReadAt) >= ThreadStatusFreshness
}

func (c *Conversation) PackSummaryIsStale(now time.Time) bool {
	if c.PackSummary == nil || c.PackSummaryRefreshedAt == nil {
		return true
	}
	return now.Sub(*c.PackSummaryRefreshedAt) >= ProfileFreshness
}

func PackKeyFor(packID, orderID string) (string, error) {
	if packID != "" {
		if !ValidNumericID(packID) {
			return "", fmt.Errorf("%w: pack %q", ErrPackUnknown, packID)
		}
		return packID, nil
	}
	if ValidNumericID(orderID) {
		return orderID, nil
	}
	return "", ErrPackUnknown
}

var threadResourcePattern = regexp.MustCompile(`^/packs/([0-9]{1,20})/sellers?/([0-9]{1,20})$`)

func ParseThreadResource(resource string) (packKey, sellerID string, err error) {
	m := threadResourcePattern.FindStringSubmatch(resource)
	if m == nil {
		return "", "", fmt.Errorf("%w: thread %q", ErrInvalidResource, resource)
	}
	return m[1], m[2], nil
}

func ThreadResource(packKey, sellerID string) string {
	return "/packs/" + packKey + "/sellers/" + sellerID
}
