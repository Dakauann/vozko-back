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
		Tasks:         []string{"MANAGE", "ADVERTISE", "ANALYZE"},
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

func TestWithoutPaymentMethodNothingCanBePublishedEvenPaused(t *testing.T) {
	a := spendableAccount()
	a.HasFunding = false
	if err := a.CanSpend(); !errors.Is(err, ErrNoFundingSource) {
		t.Fatalf("account without payment method allowed to publish: %v", err)
	}
	if err := a.CanManage(); err != nil {
		t.Fatalf("pausing and editing refused: %v", err)
	}
}

func TestRoleComesFromTheTasksMetaGrantsTheConnectedUser(t *testing.T) {
	cases := []struct {
		tasks []string
		want  AccountRole
	}{
		{[]string{"MANAGE", "ADVERTISE", "ANALYZE"}, RoleAdmin},
		{[]string{"ADVERTISE", "ANALYZE"}, RoleAdvertiser},
		{[]string{"advertise"}, RoleAdvertiser},
		{[]string{"ANALYZE"}, RoleReadOnly},
		{[]string{"DRAFT", "ANALYZE"}, RoleReadOnly},
		{[]string{"SOMETHING_NEW"}, RoleReadOnly},
		{nil, RoleReadOnly},
	}
	for _, c := range cases {
		if got := RoleFromTasks(c.tasks); got != c.want {
			t.Fatalf("%v: got %s, want %s", c.tasks, got, c.want)
		}
	}
}

func TestReadOnlyAccountReadsButRefusesEveryWrite(t *testing.T) {
	a := spendableAccount()
	a.Tasks = []string{"ANALYZE"}
	if err := a.CanRead(); err != nil {
		t.Fatalf("read refused: %v", err)
	}
	for name, check := range map[string]func() error{
		"manage":      a.CanManage,
		"spend":       a.CanSpend,
		"billing":     a.CanChangeBilling,
		"write use":   func() error { return a.Allows(UseWrite) },
		"billing use": func() error { return a.Allows(UseBilling) },
		"unknown use": func() error { return a.Allows(AccountUse(0)) },
	} {
		if err := check(); !errors.Is(err, ErrAccountReadOnly) {
			t.Fatalf("%s: got %v, want read only", name, err)
		}
	}
	if err := a.Allows(UseRead); err != nil {
		t.Fatalf("read use refused: %v", err)
	}
}

func TestAccountWhoseRoleWasNeverReadIsReadOnly(t *testing.T) {
	a := spendableAccount()
	a.Tasks = nil
	if err := a.CanManage(); !errors.Is(err, ErrAccountReadOnly) {
		t.Fatalf("unknown role allowed to write: %v", err)
	}
}

func TestOnlyAdminsChangeBillingSettings(t *testing.T) {
	a := spendableAccount()
	a.Tasks = []string{"ADVERTISE", "ANALYZE"}
	if err := a.CanSpend(); err != nil {
		t.Fatalf("advertiser cannot spend: %v", err)
	}
	if err := a.CanChangeBilling(); !errors.Is(err, ErrAccountAdminRequired) {
		t.Fatalf("advertiser changed billing: %v", err)
	}
	a.Tasks = []string{"MANAGE"}
	if err := a.CanChangeBilling(); err != nil {
		t.Fatalf("admin refused: %v", err)
	}
}

func TestReadsNeedOnlyTheConnection(t *testing.T) {
	a := spendableAccount()
	a.Connection = ConnectionNeedsReconnect
	if err := a.Allows(UseRead); !errors.Is(err, ErrAccountNeedsReconnect) {
		t.Fatalf("disconnected read allowed: %v", err)
	}
}

func TestEachUseAsksMetaForTheMatchingScope(t *testing.T) {
	if UseRead.Scope() != ScopeAdsRead {
		t.Fatalf("read scope %s", UseRead.Scope())
	}
	for _, use := range []AccountUse{UseWrite, UseBilling, AccountUse(0)} {
		if use.Scope() != ScopeAdsManagement {
			t.Fatalf("use %d scope %s", use, use.Scope())
		}
	}
}
