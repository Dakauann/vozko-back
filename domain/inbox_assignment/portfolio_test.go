package inbox_assignment

import (
	"testing"

	"vozko/domain/conversation"
)

type fakePortfolioReceivers struct {
	member  bool
	scope   conversation.DepartmentAccessScope
	granted map[string]bool
}

func (f fakePortfolioReceivers) GetDepartmentScope(string, string, bool) (conversation.DepartmentAccessScope, bool) {
	return f.scope, f.member
}

func (f fakePortfolioReceivers) HasWorkspacePermission(_, _, resource, action string, _ bool) bool {
	return f.granted[resource+":"+action]
}

func carteiraHolder() fakePortfolioReceivers {
	return fakePortfolioReceivers{
		member: true,
		scope:  conversation.DepartmentAccessScope{DepartmentIDs: []string{"sales"}, Restrict: true, WorkspaceHasDepartments: true},
		granted: map[string]bool{
			"customer_portfolios:read": true,
			"customer_portfolios:use":  true,
			"conversations:read":       true,
		},
	}
}

func TestCanReceivePortfolio(t *testing.T) {
	without := func(permission string) fakePortfolioReceivers {
		r := carteiraHolder()
		r.granted = map[string]bool{}
		for k, v := range carteiraHolder().granted {
			r.granted[k] = v && k != permission
		}
		return r
	}
	notMember := carteiraHolder()
	notMember.member = false

	cases := []struct {
		name       string
		receivers  PortfolioReceivers
		holder     string
		department string
		want       bool
	}{
		{"a holder in the conversation's department receives it, online or not", carteiraHolder(), "u1", "sales", true},
		{"a conversation without department reaches the holder", carteiraHolder(), "u1", "", true},
		{"a holder outside the conversation's department goes back to the roulette", carteiraHolder(), "u1", "support", false},
		{"a holder who left the workspace receives nothing", notMember, "u1", "sales", false},
		{"a holder who lost the carteira permission receives nothing", without("customer_portfolios:use"), "u1", "sales", false},
		{"a holder who can no longer read conversations receives nothing", without("conversations:read"), "u1", "sales", false},
		{"no holder", carteiraHolder(), "", "sales", false},
		{"no way to check the holder", nil, "u1", "sales", false},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if got := CanReceivePortfolio(tc.receivers, tc.holder, "ws-1", tc.department); got != tc.want {
				t.Fatalf("CanReceivePortfolio = %v, want %v", got, tc.want)
			}
		})
	}
}
