package advertising

import (
	"errors"
	"testing"
)

func spendableAccount() *AdAccount {
	return &AdAccount{
		MetaAccountID: "act_123",
		Currency:      "BRL",
		Timezone:      "America/Sao_Paulo",
		MetaStatus:    MetaAccountActive,
		HasFunding:    true,
		Connection:    ConnectionConnected,
	}
}

func TestAccountIDIsStoredWithoutTheActPrefix(t *testing.T) {
	if got := NormalizeAccountID(" act_123 "); got != "123" {
		t.Fatalf("got %q", got)
	}
	if got := spendableAccount().GraphID(); got != "act_123" {
		t.Fatalf("graph id %q", got)
	}
}

func TestHealthyFundedAccountCanSpend(t *testing.T) {
	if err := spendableAccount().CanSpend(); err != nil {
		t.Fatalf("refused: %v", err)
	}
}

func TestGracePeriodStillDelivers(t *testing.T) {
	a := spendableAccount()
	a.MetaStatus = MetaAccountInGracePeriod
	if err := a.CanSpend(); err != nil {
		t.Fatalf("refused: %v", err)
	}
}

func TestAccountThatCannotPayIsRefusedBeforeMetaParksTheAd(t *testing.T) {
	cases := []struct {
		name   string
		mutate func(*AdAccount)
		want   error
	}{
		{"no payment method", func(a *AdAccount) { a.HasFunding = false }, ErrNoFundingSource},
		{"disabled", func(a *AdAccount) { a.MetaStatus = MetaAccountDisabled }, ErrAccountNotActive},
		{"unsettled", func(a *AdAccount) { a.MetaStatus = MetaAccountUnsettled }, ErrAccountNotActive},
		{"status never read", func(a *AdAccount) { a.MetaStatus = 0 }, ErrAccountNotActive},
		{"needs reconnect", func(a *AdAccount) { a.Connection = ConnectionNeedsReconnect }, ErrAccountNeedsReconnect},
		{"unknown currency", func(a *AdAccount) { a.Currency = "" }, ErrUnknownCurrency},
		{"unknown timezone", func(a *AdAccount) { a.Timezone = "Mars/Base" }, ErrUnknownTimezone},
	}
	for _, c := range cases {
		a := spendableAccount()
		c.mutate(a)
		if err := a.CanSpend(); !errors.Is(err, c.want) {
			t.Fatalf("%s: got %v, want %v", c.name, err, c.want)
		}
	}
}

func TestPausingWorksOnAccountsThatCannotSpend(t *testing.T) {
	a := spendableAccount()
	a.HasFunding = false
	a.MetaStatus = MetaAccountUnsettled
	if err := a.CanManage(); err != nil {
		t.Fatalf("manage refused: %v", err)
	}
	a.Connection = ConnectionDisconnected
	if err := a.CanManage(); !errors.Is(err, ErrAccountNeedsReconnect) {
		t.Fatalf("disconnected account managed: %v", err)
	}
	var none *AdAccount
	if err := none.CanManage(); !errors.Is(err, ErrAccountNotFound) {
		t.Fatalf("nil account: %v", err)
	}
}
