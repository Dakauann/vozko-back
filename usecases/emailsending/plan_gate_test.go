package emailsending

import (
	"context"
	"errors"
	"testing"

	es "vozko/domain/emailsending"
	workspace_plan "vozko/domain/workspace/workspace_plan"
)

type fakePlans struct {
	refused map[string]error
	reads   int
}

func (p *fakePlans) Execute(workspaceID string) (*workspace_plan.WorkspaceSubscription, error) {
	p.reads++
	if err, ok := p.refused[workspaceID]; ok {
		return nil, err
	}
	return &workspace_plan.WorkspaceSubscription{WorkspaceID: workspaceID, Status: workspace_plan.SubscriptionStatusActive}, nil
}

func TestActivationWithoutAPaidPlanIsRefusedBeforeAnyWork(t *testing.T) {
	cases := []struct {
		name string
		plan error
		want error
	}{
		{"no current plan", workspace_plan.ErrSubscriptionNotCurrent, es.ErrPlanRequired},
		{"a cancelled plan", workspace_plan.ErrSubscriptionNotActive, es.ErrPlanRequired},
		{"the plan cannot be read", errors.New("database down"), es.ErrPlanUnreadable},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			h := newHarness(t)
			h.plans.refused[wsID] = tc.plan
			_, err := h.m.Setup.Provision(context.Background(), wsID, "")
			if !errors.Is(err, tc.want) {
				t.Fatalf("err = %v, want %v", err, tc.want)
			}
			if len(h.accounts.rows) != 0 || len(h.domains.rows) != 0 || len(h.wake.take()) != 0 {
				t.Fatal("a refused activation created rows or queued a setup run")
			}
		})
	}
}

func TestTheSetupStatusSaysUpFrontWhetherActivationIsAllowed(t *testing.T) {
	h := newHarness(t)
	ctx := context.Background()
	view, err := h.m.Setup.Status(ctx, wsID)
	if err != nil || view.Activation != nil {
		t.Fatalf("paid workspace: activation %v err %v", view.Activation, err)
	}
	h.plans.refused[wsID] = workspace_plan.ErrSubscriptionNotCurrent
	view, err = h.m.Setup.Status(ctx, wsID)
	if err != nil || !errors.Is(view.Activation, es.ErrPlanRequired) || view.Subdomain.Slug == "" {
		t.Fatalf("unpaid workspace: %+v err %v", view, err)
	}
	h.plans.refused[wsID] = errors.New("database down")
	view, err = h.m.Setup.Status(ctx, wsID)
	if err != nil || !errors.Is(view.Activation, es.ErrPlanUnreadable) {
		t.Fatalf("unreadable plan: activation %v err %v, want the status to answer and refuse", view.Activation, err)
	}
}

func TestAnActiveAccountThatLosesItsPlanKeepsWorkingButCannotGrow(t *testing.T) {
	h := newHarness(t)
	ctx := context.Background()
	h.provisionVerified(t, wsID)
	h.plans.refused[wsID] = workspace_plan.ErrSubscriptionNotCurrent

	view, err := h.m.Setup.Status(ctx, wsID)
	if err != nil || !view.Provisioned || len(view.Domains) != 1 || !view.Domains[0].Domain.Sendable() {
		t.Fatalf("the active account must stay as it was: %+v err %v", view, err)
	}
	if !errors.Is(view.Activation, es.ErrPlanRequired) {
		t.Fatalf("activation = %v", view.Activation)
	}
	slug := view.Account.Slug
	if _, err := h.m.Setup.ChangeSubdomain(ctx, wsID, "outro-nome"); !errors.Is(err, es.ErrPlanRequired) {
		t.Fatalf("change subdomain = %v", err)
	}
	if acct, _ := h.accounts.GetByWorkspace(ctx, wsID); acct.Slug != slug {
		t.Fatalf("slug changed to %s", acct.Slug)
	}
	if _, err := h.m.Domains.Connect(ctx, wsID, "lojadajoana.com.br"); !errors.Is(err, es.ErrPlanRequired) {
		t.Fatalf("connect = %v", err)
	}
	if _, err := h.domains.GetByDomain(ctx, "lojadajoana.com.br"); !errors.Is(err, es.ErrDomainNotFound) {
		t.Fatal("a refused connection stored the domain")
	}
	if _, err := h.m.Setup.Provision(ctx, wsID, ""); !errors.Is(err, es.ErrPlanRequired) {
		t.Fatalf("provision again = %v", err)
	}
	if len(h.tenants.retired) != 0 || len(h.provider.removedIdents) != 0 {
		t.Fatal("losing the plan must never tear the account down")
	}
}
