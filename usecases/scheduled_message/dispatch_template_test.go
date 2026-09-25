package scheduled_message_usecase

import (
	"context"
	"errors"
	"testing"

	"vozko/domain/balance"
	sm "vozko/domain/scheduled_message"
	"vozko/domain/whatsapp/template"
	wo "vozko/domain/whatsapp_outreach"
	"vozko/domain/workspace"
	"vozko/domain/workspace/workspace_plan"
)

func TestDispatchATemplateWhileTheWindowIsClosed(t *testing.T) {
	f := newDispatchFixture(t)
	f.repo.put(templateMessage("sched-1", fixedNow))
	f.windows.set(false, nil)

	if err := f.uc.Execute(context.Background(), "sched-1"); err != nil {
		t.Fatalf("Execute: %v", err)
	}

	if f.templates.sendCount() != 1 {
		t.Fatalf("template sends = %d, want 1", f.templates.sendCount())
	}
	if f.send.count() != 0 {
		t.Error("a template must not go through the free-form send path")
	}
	sent := f.templates.sends[0]
	if sent.IdempotencyKey != "scheduled-message:sched-1" {
		t.Errorf("key = %q, want one stable per scheduled message so a redelivery never charges twice", sent.IdempotencyKey)
	}
	if sent.UserID != "user-1" || sent.TemplateID != "tpl-1" || sent.BodyParams[0] != "Ana" {
		t.Errorf("the template went out differently from how it was scheduled: %+v", sent)
	}
	if got := f.repo.get("sched-1").Status; got != sm.StatusSent {
		t.Errorf("status = %q, want sent", got)
	}
}

func TestDispatchATemplateChecksTheCreatorStillMaySendTemplates(t *testing.T) {
	f := newDispatchFixture(t)
	f.repo.put(templateMessage("sched-1", fixedNow))

	if err := f.uc.Execute(context.Background(), "sched-1"); err != nil {
		t.Fatalf("Execute: %v", err)
	}
	want := "user-1|ws-1|" + string(workspace.ResourceWhatsAppTemplates) + ":" + string(workspace.ActionSend)
	if len(f.permissions.checked) != 1 || f.permissions.checked[0] != want {
		t.Fatalf("permission checks = %v, want %q", f.permissions.checked, want)
	}
}

func TestDispatchATextDoesNotAskForTemplatePermission(t *testing.T) {
	f := newDispatchFixture(t)
	f.pending("sched-1")

	if err := f.uc.Execute(context.Background(), "sched-1"); err != nil {
		t.Fatalf("Execute: %v", err)
	}
	if f.permissions.calls() != 0 {
		t.Error("free text needs no template permission")
	}
}

func TestDispatchATemplateAfterThePermissionWasRevoked(t *testing.T) {
	for _, denial := range []error{workspace.ErrInsufficientPermissions, workspace.ErrUnauthorized} {
		t.Run(denial.Error(), func(t *testing.T) {
			f := newDispatchFixture(t)
			f.repo.put(templateMessage("sched-1", fixedNow))
			f.permissions.err = denial

			if err := f.uc.Execute(context.Background(), "sched-1"); err != nil {
				t.Fatalf("Execute: %v", err)
			}
			if f.templates.sendCount() != 0 {
				t.Fatal("a template went out, and was charged, for someone no longer allowed to send it")
			}
			stored := f.repo.get("sched-1")
			if stored.FailureReason == nil || *stored.FailureReason != sm.ReasonPermissionRevoked {
				t.Errorf("reason = %v, want permission_revoked", stored.FailureReason)
			}
		})
	}
}

func TestDispatchATemplateWhenThePermissionCannotBeRead(t *testing.T) {
	f := newDispatchFixture(t)
	f.repo.put(templateMessage("sched-1", fixedNow))
	f.permissions.err = errors.New("db down")

	_ = f.uc.Execute(context.Background(), "sched-1")

	if f.templates.sendCount() != 0 {
		t.Fatal("an unreadable permission must not be treated as granted")
	}
	if got := f.repo.get("sched-1").Status; got != sm.StatusFailed {
		t.Errorf("status = %q, want failed", got)
	}
}

func TestDispatchClassifiesTemplateSendErrors(t *testing.T) {
	cases := []struct {
		name string
		err  error
		want sm.FailureReason
	}{
		{"blocked contact", wo.ErrLeadBlocked, sm.ReasonContactIneligible},
		{"inside the spam window", wo.ErrWithinSpamWindow, sm.ReasonContactIneligible},
		{"template paused", template.ErrTemplateNotSendable, sm.ReasonTemplateUnavailable},
		{"template of another account", template.ErrTemplatePhoneMismatch, sm.ReasonTemplateUnavailable},
		{"template variables changed", template.ErrTemplateParamsMismatch, sm.ReasonTemplateUnavailable},
		{"grant withdrawn", wo.ErrTemplateForbidden, sm.ReasonTemplateUnavailable},
		{"template deleted", wo.ErrTemplateNotFound, sm.ReasonTemplateUnavailable},
		{"no money", balance.ErrInsufficientBalance, sm.ReasonInsufficientBalance},
		{"no balance row", balance.ErrBalanceNotFound, sm.ReasonInsufficientBalance},
		{"no price", template.ErrPricingUnavailable, sm.ReasonBillingUnavailable},
		{"no price on the ledger", balance.ErrPriceUnavailable, sm.ReasonBillingUnavailable},
		{"subscription lapsed", workspace_plan.ErrSubscriptionNotCurrent, sm.ReasonBillingUnavailable},
		{"subscription inactive", workspace_plan.ErrSubscriptionNotActive, sm.ReasonBillingUnavailable},
		{"answer lost", wo.ErrSendOutcomeUnknown, sm.ReasonOutcomeUnknown},
		{"conversation gone", wo.ErrConversationNotFound, sm.ReasonEntryUnavailable},
		{"number disconnected", wo.ErrPhoneNotConnected, sm.ReasonEntryUnavailable},
		{"number withdrawn", wo.ErrBusinessPhoneNotFound, sm.ReasonEntryUnavailable},
		{"rejected by Meta", errors.New("131026 message undeliverable"), sm.ReasonProviderError},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			f := newDispatchFixture(t)
			f.repo.put(templateMessage("sched-1", fixedNow))
			f.templates.sendErr = tc.err

			if err := f.uc.Execute(context.Background(), "sched-1"); err != nil {
				t.Fatalf("Execute: %v", err)
			}
			stored := f.repo.get("sched-1")
			if stored.Status != sm.StatusFailed {
				t.Fatalf("status = %q, want failed", stored.Status)
			}
			if stored.FailureReason == nil || *stored.FailureReason != tc.want {
				t.Errorf("reason = %v, want %v", stored.FailureReason, tc.want)
			}
		})
	}
}
