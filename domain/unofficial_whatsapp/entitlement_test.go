package unofficial_whatsapp

import (
	"errors"
	"testing"
)

// The allowance arithmetic is where the product's promise lives: "you have N
// numbers included, buy more to get more". Every case below is one an operator
// or a support agent will actually hit.
func TestInstanceAllowance(t *testing.T) {
	cases := []struct {
		name          string
		allowance     InstanceAllowance
		wantRemaining int
		wantCanAdd    bool
		wantOver      bool
	}{
		{
			name:          "granted nothing",
			allowance:     InstanceAllowance{Limit: 0, Used: 0},
			wantRemaining: 0,
			wantCanAdd:    false,
		},
		{
			name:          "room to spare",
			allowance:     InstanceAllowance{Limit: 5, Used: 2},
			wantRemaining: 3,
			wantCanAdd:    true,
		},
		{
			// The boundary that decides whether "5 included" means five or six.
			name:          "exactly at the limit",
			allowance:     InstanceAllowance{Limit: 5, Used: 5},
			wantRemaining: 0,
			wantCanAdd:    false,
		},
		{
			name:          "one slot left",
			allowance:     InstanceAllowance{Limit: 1, Used: 0},
			wantRemaining: 1,
			wantCanAdd:    true,
		},
		{
			// Reachable without anyone doing anything wrong: an addon lapses, or
			// an administrator lowers a grant. Remaining must not go negative —
			// "-2 numbers left" is not a thing any screen can render.
			name:          "over the limit after a reduction",
			allowance:     InstanceAllowance{Limit: 2, Used: 5},
			wantRemaining: 0,
			wantCanAdd:    false,
			wantOver:      true,
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if got := tc.allowance.Remaining(); got != tc.wantRemaining {
				t.Errorf("Remaining() = %d, want %d", got, tc.wantRemaining)
			}
			if got := tc.allowance.CanProvision(); got != tc.wantCanAdd {
				t.Errorf("CanProvision() = %v, want %v", got, tc.wantCanAdd)
			}
			if got := tc.allowance.OverLimit(); got != tc.wantOver {
				t.Errorf("OverLimit() = %v, want %v", got, tc.wantOver)
			}
		})
	}
}

// The two refusals must stay distinguishable, because their remedies are.
//
// "You have none" needs an allowance granted by us; "you have none left" needs
// an addon the workspace can buy themselves. Collapsing them into one error
// sends both to support, and the second one did not need to go there.
func TestEnforceDistinguishesNoneFromNoneLeft(t *testing.T) {
	none := InstanceAllowance{Limit: 0, Used: 0}.Enforce()
	if !errors.Is(none, ErrNoInstanceAllowance) {
		t.Errorf("a workspace granted nothing got %v, want ErrNoInstanceAllowance", none)
	}

	full := InstanceAllowance{Limit: 3, Used: 3}.Enforce()
	if !errors.Is(full, ErrInstanceLimitReached) {
		t.Errorf("a full workspace got %v, want ErrInstanceLimitReached", full)
	}
	// The counts ride along, because "all your numbers are in use" without
	// saying how many is a support ticket rather than an answer.
	if msg := full.Error(); msg == ErrInstanceLimitReached.Error() {
		t.Error("the refusal does not say how many of how many are in use")
	}

	if err := (InstanceAllowance{Limit: 3, Used: 1}).Enforce(); err != nil {
		t.Errorf("a workspace with room was refused: %v", err)
	}
}

// A workspace that is over its limit is still refused a NEW number — the
// overage is not a credit.
func TestOverLimitStillRefuses(t *testing.T) {
	err := InstanceAllowance{Limit: 2, Used: 7}.Enforce()
	if !errors.Is(err, ErrInstanceLimitReached) {
		t.Errorf("an over-limit workspace got %v, want a refusal", err)
	}
}
