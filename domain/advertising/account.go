package advertising

import (
	"fmt"
	"strings"
	"time"
)

type MetaAccountStatus int

const (
	MetaAccountActive            MetaAccountStatus = 1
	MetaAccountDisabled          MetaAccountStatus = 2
	MetaAccountUnsettled         MetaAccountStatus = 3
	MetaAccountPendingRiskReview MetaAccountStatus = 7
	MetaAccountPendingSettlement MetaAccountStatus = 8
	MetaAccountInGracePeriod     MetaAccountStatus = 9
	MetaAccountPendingClosure    MetaAccountStatus = 100
	MetaAccountClosed            MetaAccountStatus = 101
)

func (s MetaAccountStatus) Delivers() bool {
	return s == MetaAccountActive || s == MetaAccountInGracePeriod
}

func (s MetaAccountStatus) Key() string {
	switch s {
	case MetaAccountActive:
		return "active"
	case MetaAccountDisabled:
		return "disabled"
	case MetaAccountUnsettled:
		return "unsettled"
	case MetaAccountPendingRiskReview:
		return "pending_risk_review"
	case MetaAccountPendingSettlement:
		return "pending_settlement"
	case MetaAccountInGracePeriod:
		return "in_grace_period"
	case MetaAccountPendingClosure:
		return "pending_closure"
	case MetaAccountClosed:
		return "closed"
	}
	return "unknown"
}

type Connection string

const (
	ConnectionConnected      Connection = "CONNECTED"
	ConnectionNeedsReconnect Connection = "NEEDS_RECONNECT"
	ConnectionDisconnected   Connection = "DISCONNECTED"
)

type AdAccount struct {
	ID            string
	WorkspaceID   string
	GrantID       string
	MetaAccountID string
	Name          string
	BusinessID    string
	BusinessName  string
	Currency      string
	Timezone      string
	MetaStatus    MetaAccountStatus
	DisableReason int
	HasFunding    bool
	AmountSpent   int64
	SpendCap      int64
	Tasks         []string
	Connection    Connection
	LastSyncedAt  *time.Time
	CreatedAt     time.Time
	UpdatedAt     time.Time
}

func (a *AdAccount) SpendCapLimit() *int64 {
	if a.SpendCap <= 0 {
		return nil
	}
	limit := a.SpendCap
	return &limit
}

func NormalizeAccountID(raw string) string {
	return strings.TrimPrefix(strings.TrimSpace(raw), "act_")
}

func (a *AdAccount) GraphID() string { return "act_" + NormalizeAccountID(a.MetaAccountID) }

func (a *AdAccount) Location() (*time.Location, error) {
	if a == nil || strings.TrimSpace(a.Timezone) == "" {
		return nil, ErrUnknownTimezone
	}
	loc, err := time.LoadLocation(a.Timezone)
	if err != nil {
		return nil, fmt.Errorf("%w: %q", ErrUnknownTimezone, a.Timezone)
	}
	return loc, nil
}

type spendCheck struct {
	item  ReadinessKey
	check func(*AdAccount) error
}

var spendChecks = []spendCheck{
	{ReadyConnection, (*AdAccount).CanRead},
	{ReadyRole, (*AdAccount).checkRole},
	{ReadyAccountStatus, (*AdAccount).checkStatus},
	{ReadyAccountDetails, (*AdAccount).checkDetails},
	{ReadyPaymentMethod, (*AdAccount).checkFunding},
}

func (a *AdAccount) CanSpend() error {
	for _, c := range spendChecks {
		if err := c.check(a); err != nil {
			return err
		}
	}
	return nil
}

func (a *AdAccount) checkStatus() error {
	if !a.MetaStatus.Delivers() {
		return fmt.Errorf("%w: %s", ErrAccountNotActive, a.MetaStatus.Key())
	}
	return nil
}

func (a *AdAccount) checkDetails() error {
	if _, err := NormalizeCurrency(a.Currency); err != nil {
		return err
	}
	_, err := a.Location()
	return err
}

func (a *AdAccount) checkFunding() error {
	if !a.HasFunding {
		return ErrNoFundingSource
	}
	return nil
}
