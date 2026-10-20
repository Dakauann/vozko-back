package mercadolivre

import (
	"fmt"
	"regexp"
	"time"
)

const (
	ThreadStatusActive  = "active"
	ThreadStatusBlocked = "blocked"
)

type Substatus string

const (
	SubstatusBlockedByTime                               Substatus = "blocked_by_time"
	SubstatusBlockedByBuyer                              Substatus = "blocked_by_buyer"
	SubstatusBlockedByMediation                          Substatus = "blocked_by_mediation"
	SubstatusBlockedByMediationFBM                       Substatus = "blocked_by_mediation_fbm"
	SubstatusBlockedByFulfillment                        Substatus = "blocked_by_fulfillment"
	SubstatusBlockedByPayment                            Substatus = "blocked_by_payment"
	SubstatusBlockedByConversationInitiatedBySeller      Substatus = "blocked_by_conversation_initiated_by_seller"
	SubstatusBlockedByConversationUseMessageAPI          Substatus = "blocked_by_conversation_use_message_api"
	SubstatusBlockedByConversationInitiatedBySellerLimit Substatus = "blocked_by_conversation_initiated_by_seller_limited"
	SubstatusBlockedByCancelledOrder                     Substatus = "blocked_by_cancelled_order"
	SubstatusBlockedByCancelledOrderByFraud              Substatus = "blocked_by_cancelled_order_by_fraud"
	SubstatusBlockedByCancelledOrderHidden               Substatus = "blocked_by_cancelled_order_hidden"
	SubstatusBlockedByConversationExpired                Substatus = "blocked_by_conversation_expired"
	SubstatusBlockedByRefund                             Substatus = "blocked_by_refund"
	SubstatusBlockedByClaimChangeOpen                    Substatus = "blocked_by_claim_change_open"
	SubstatusBlockedByClaimChangeClosed                  Substatus = "blocked_by_claim_change_closed"
	SubstatusBlockedByDeactivatedAccount                 Substatus = "blocked_by_deactivated_account"
	SubstatusBlockedByRestrictions                       Substatus = "blocked_by_restrictions"
	SubstatusBlockedByMessagePendingReview               Substatus = "blocked_by_message_pending_review"
	SubstatusBlockedByReturnToBuyerFulfillment           Substatus = "blocked_by_return_to_buyer_fulfillment"
	SubstatusBlockedByAIAssistant                        Substatus = "blocked_by_ai_assistant"
	SubstatusBlockedByAIAssistantExpired                 Substatus = "blocked_by_ai_assistant_expired"
	SubstatusBlockedByAIAssistantContactClosed           Substatus = "blocked_by_ai_assistant_contact_closed"
	SubstatusBlockByAIAssistantInitiatedBySellerExpired  Substatus = "block_by_ai_assistant_initiated_by_seller_expired"
	SubstatusBlockedByResale                             Substatus = "blocked_by_resale"
	SubstatusBlockedByProximityGroceries                 Substatus = "blocked_by_proximity_groceries"
)

var substatusTable = []Substatus{
	SubstatusBlockedByTime,
	SubstatusBlockedByBuyer,
	SubstatusBlockedByMediation,
	SubstatusBlockedByMediationFBM,
	SubstatusBlockedByFulfillment,
	SubstatusBlockedByPayment,
	SubstatusBlockedByConversationInitiatedBySeller,
	SubstatusBlockedByConversationUseMessageAPI,
	SubstatusBlockedByConversationInitiatedBySellerLimit,
	SubstatusBlockedByCancelledOrder,
	SubstatusBlockedByCancelledOrderByFraud,
	SubstatusBlockedByCancelledOrderHidden,
	SubstatusBlockedByConversationExpired,
	SubstatusBlockedByRefund,
	SubstatusBlockedByClaimChangeOpen,
	SubstatusBlockedByClaimChangeClosed,
	SubstatusBlockedByDeactivatedAccount,
	SubstatusBlockedByRestrictions,
	SubstatusBlockedByMessagePendingReview,
	SubstatusBlockedByReturnToBuyerFulfillment,
	SubstatusBlockedByAIAssistant,
	SubstatusBlockedByAIAssistantExpired,
	SubstatusBlockedByAIAssistantContactClosed,
	SubstatusBlockByAIAssistantInitiatedBySellerExpired,
	SubstatusBlockedByResale,
	SubstatusBlockedByProximityGroceries,
}

var knownSubstatuses = func() map[Substatus]struct{} {
	m := make(map[Substatus]struct{}, len(substatusTable))
	for _, s := range substatusTable {
		m[s] = struct{}{}
	}
	return m
}()

func Substatuses() []Substatus {
	out := make([]Substatus, len(substatusTable))
	copy(out, substatusTable)
	return out
}

func KnownSubstatus(raw string) bool {
	_, ok := knownSubstatuses[Substatus(raw)]
	return ok
}

func WindowFromStatus(status, substatus string) (open bool, detail string, err error) {
	switch status {
	case ThreadStatusActive:
		if substatus == "" {
			return true, "", nil
		}
	case ThreadStatusBlocked:
		if substatus == "" {
			return false, "", fmt.Errorf("%w: blocked without a substatus", ErrUnknownSubstatus)
		}
	default:
		return false, "", fmt.Errorf("%w: %q", ErrUnknownThreadStatus, status)
	}
	if !KnownSubstatus(substatus) {
		return false, "", fmt.Errorf("%w: %q", ErrUnknownSubstatus, substatus)
	}
	return false, substatus, nil
}

func BudgetFor(agentMediated *bool) (int, error) {
	if agentMediated == nil {
		return 0, ErrMediationUnknown
	}
	if *agentMediated {
		return 1, nil
	}
	return 0, nil
}

var agentUserIDs = map[string]string{
	SiteMLB: "3037675074",
}

func AgentUserID(site string) (string, bool) {
	id, ok := agentUserIDs[site]
	return id, ok
}

func IsAgentUserID(site, userID string) bool {
	id, ok := agentUserIDs[site]
	return ok && userID != "" && id == userID
}

func SendTarget(conv *Conversation, siteID, sellerID string) (string, error) {
	if conv == nil || conv.AgentMediated == nil {
		return "", ErrMediationUnknown
	}
	if *conv.AgentMediated {
		id, ok := AgentUserID(siteID)
		if !ok {
			return "", fmt.Errorf("%w: no messaging agent for %q", ErrSiteNotSupported, siteID)
		}
		return id, nil
	}
	buyer := conv.BuyerMLUserID
	if !ValidNumericID(buyer) || buyer == sellerID {
		return "", ErrBuyerUnknown
	}
	return buyer, nil
}

var (
	directThreadPath   = regexp.MustCompile(`^/packs/[0-9]{1,20}/sellers?/[0-9]{1,20}$`)
	mediatedThreadPath = regexp.MustCompile(`^/packs/[0-9]{1,20}/sellers/[0-9]{1,20}/conversations/[A-Za-z0-9_-]{1,64}$`)
)

func MediationFromPath(path string) *bool {
	var mediated bool
	switch {
	case mediatedThreadPath.MatchString(path):
		mediated = true
	case directThreadPath.MatchString(path):
		mediated = false
	default:
		return nil
	}
	return &mediated
}

type ThreadState struct {
	Status         string
	Substatus      string
	Detail         string
	AgentMediated  *bool
	OutboundBudget int
	WindowOpen     bool
	ReadAt         time.Time
}

func NewThreadState(status, substatus string, agentMediated *bool, readAt time.Time) (ThreadState, error) {
	state := ThreadState{Status: status, Substatus: substatus, AgentMediated: agentMediated, ReadAt: readAt}
	open, detail, err := WindowFromStatus(status, substatus)
	state.Detail = detail
	if err != nil {
		return state, err
	}
	budget, err := BudgetFor(agentMediated)
	if err != nil {
		return state, err
	}
	state.OutboundBudget = budget
	state.WindowOpen = open
	return state, nil
}
