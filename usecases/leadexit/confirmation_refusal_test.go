package leadexit_usecase

import (
	"context"
	"errors"
	"testing"

	"vozko/domain/buttonaction"
	"vozko/domain/lead"
	"vozko/domain/leadexit"
)

func handOffWithConfirmation() buttonaction.ButtonAction {
	return buttonaction.ButtonAction{ButtonIndex: 0, ButtonText: "Falar com atendente", Kind: buttonaction.KindHandOff,
		Params: buttonaction.Params{DepartmentID: "dep-1"}, Confirmation: "Já chamamos alguém."}
}

func TestANonExitConfirmationIsNotSentToALeadWhoCannotReceive(t *testing.T) {
	for _, tc := range []struct {
		name   string
		mark   func(*lead.Lead)
		reason string
	}{
		{"opted out", func(l *lead.Lead) { at := stepNow; l.OptedOutAt = &at; l.OptOutSource = lead.OptOutLeadRequest }, "lead_opted_out"},
		{"blocked", func(l *lead.Lead) { l.Blocked = true }, "lead_blocked"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			stored := storedLead("lead-1", leadNumber)
			tc.mark(stored)
			f := newFixture(t, stored)
			f.sentPromo("wamid.tmpl", leadNumber)
			f.promoButton("vz1:0", handOffWithConfirmation())
			replier := &fakeReplier{}

			tap(t, f, replier)

			if len(f.handOff.roulette) != 1 {
				t.Fatalf("the hand off itself must still run: %+v", f.handOff.roulette)
			}
			if len(replier.sent) != 0 {
				t.Fatalf("a confirmation went out: %+v", replier.sent)
			}
			run := f.runs.of("wamid.in")
			if run.Confirmation != leadexit.ConfirmationNotSent || run.ConfirmationReason != tc.reason {
				t.Fatalf("run = %+v", run)
			}
			if line := f.events.last(t); line["confirmation"] != "not_sent" || line["confirmation_reason"] != tc.reason {
				t.Fatalf("line = %+v", line)
			}
		})
	}
}

type unreadableLeads struct{ *memLeads }

func (unreadableLeads) FindByID(string, string) (*lead.Lead, error) {
	return nil, errors.New("database down")
}

func TestANonExitConfirmationIsNotSentWhenTheLeadCannotBeChecked(t *testing.T) {
	f := tapFixture(t, handOffWithConfirmation())
	f.step.deps.Leads = unreadableLeads{f.leads}
	replier := &fakeReplier{}

	if _, err := f.step.Run(context.Background(), tapOn("wamid.in", "wamid.tmpl", "vz1:0", "Botão"), replier); err != nil {
		t.Fatalf("Run: %v", err)
	}

	if len(replier.sent) != 0 {
		t.Fatalf("a confirmation went out unchecked: %+v", replier.sent)
	}
	if run := f.runs.of("wamid.in"); run.Confirmation != leadexit.ConfirmationNotSent || run.ConfirmationReason != "lead_exit_action_failed" {
		t.Fatalf("run = %+v", run)
	}
}

func TestTheOptOutConfirmationStillGoesToTheLeadWhoJustLeft(t *testing.T) {
	f := sairFixture(t)
	replier := &fakeReplier{}

	if _, err := f.step.Run(context.Background(), tapOn("wamid.in", "wamid.tmpl", "vz1:1", "SAIR"), replier); err != nil {
		t.Fatal(err)
	}

	if len(replier.sent) != 1 || replier.sent[0] != sairConfirmation {
		t.Fatalf("sent = %+v", replier.sent)
	}
}
