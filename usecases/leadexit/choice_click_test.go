package leadexit_usecase

import (
	"context"
	"errors"
	"testing"

	"vozko/domain/buttonaction"
	"vozko/domain/choicestats"
	"vozko/domain/leadexit"
)

type spyChoices struct {
	clicks []choicestats.Click
}

func (s *spyChoices) RecordSend(context.Context, choicestats.Send) {}

func (s *spyChoices) RecordClick(_ context.Context, click choicestats.Click) {
	s.clicks = append(s.clicks, click)
}

func TestATemplateTapCountsOnTheRepliedSend(t *testing.T) {
	f := newFixture(t, storedLead("lead-1", leadNumber))
	spy := f.step.deps.Choices.(*spyChoices)
	f.sentPromo("wamid.promo", leadNumber)
	f.promoButton("vz1:1", buttonaction.ButtonAction{ButtonIndex: 1, ButtonText: "Quero"})

	if _, err := f.step.Run(context.Background(), tapOn("wamid.in", "wamid.promo", "vz1:1", "Quero"), &fakeReplier{}); err != nil && !errors.Is(err, leadexit.ErrRunInProgress) {
		t.Fatal(err)
	}
	if len(spy.clicks) != 1 {
		t.Fatalf("clicks = %+v", spy.clicks)
	}
	click := spy.clicks[0]
	if click.WorkspaceID != workspaceA || click.ProviderMessageID != "wamid.promo" || click.Option != "template_button:1" || click.PromptKey != "" {
		t.Fatalf("click = %+v", click)
	}
}

func TestAQuotedTextReplyToATemplateIsFreeTextAndAnUnquotedOneCountsNowhere(t *testing.T) {
	f := newFixture(t, storedLead("lead-1", leadNumber))
	spy := f.step.deps.Choices.(*spyChoices)
	f.sentPromo("wamid.promo", leadNumber)

	quoted := typed("wamid.in1", "quanto custa?")
	quoted.ContextMessageID = "wamid.promo"
	_, _ = f.step.Run(context.Background(), quoted, &fakeReplier{})
	_, _ = f.step.Run(context.Background(), typed("wamid.in2", "oi"), &fakeReplier{})

	if len(spy.clicks) != 1 || spy.clicks[0].Option != choicestats.OptionFreeText || spy.clicks[0].ProviderMessageID != "wamid.promo" {
		t.Fatalf("clicks = %+v", spy.clicks)
	}
}

func TestATapOnAMessageSentElsewhereCountsNowhere(t *testing.T) {
	f := newFixture(t, storedLead("lead-1", leadNumber))
	spy := f.step.deps.Choices.(*spyChoices)
	f.sentPromo("wamid.promo", "5511000000000")
	_, _ = f.step.Run(context.Background(), tapOn("wamid.in", "wamid.promo", "vz1:0", "Quero"), &fakeReplier{})
	if len(spy.clicks) != 0 {
		t.Fatalf("clicks = %+v", spy.clicks)
	}
}

func TestTheStepNeedsTheChoiceRecorder(t *testing.T) {
	f := newFixture(t)
	deps := f.step.deps
	deps.Choices = nil
	if _, err := NewStep(deps); !errors.Is(err, leadexit.ErrUnavailable) {
		t.Fatalf("got %v", err)
	}
}
