package messageaction

import (
	"context"
	"errors"
	"testing"
	"time"

	"vozko/domain/conversation"
)

func TestDeletingRetractsAtTheProviderThenMarksTheMessage(t *testing.T) {
	f := newActionFixture(t, ownTelegramText("m1", actionNow.Add(-time.Hour)))

	got, err := f.uc.Delete.Execute(context.Background(), ana, target("m1"))
	if err != nil {
		t.Fatal(err)
	}
	if len(f.channel.calls) != 1 || f.channel.calls[0] != "retract:pm-m1" {
		t.Fatalf("provider calls = %v", f.channel.calls)
	}
	if got.RetractedAt == nil || !got.RetractedAt.Equal(actionNow) || len(f.announcer.updated) != 1 {
		t.Fatalf("retracted %v announced %v", got.RetractedAt, f.announcer.updated)
	}
}

func TestADeleteTheProviderRefusesChangesNothing(t *testing.T) {
	f := newActionFixture(t, ownTelegramText("m1", actionNow.Add(-time.Hour)))
	f.channel.fail = errors.New("telegram said no")

	if _, err := f.uc.Delete.Execute(context.Background(), ana, target("m1")); err == nil {
		t.Fatal("the provider failure must surface")
	}
	f.requireNothingChanged(t)
}

func TestACustomerMessageCannotBeDeleted(t *testing.T) {
	f := newActionFixture(t, customerTelegramText("m1"))

	_, err := f.uc.Delete.Execute(context.Background(), ana, target("m1"))
	requireError(t, err, conversation.ErrMessageActionNotAllowed)
	if len(f.channel.calls) != 0 {
		t.Fatal("a customer message reached the provider delete")
	}
}

func TestADeletePastTheChannelWindowIsRefused(t *testing.T) {
	f := newActionFixture(t, ownTelegramText("m1", actionNow.Add(-72*time.Hour)))

	_, err := f.uc.Delete.Execute(context.Background(), ana, target("m1"))
	requireError(t, err, conversation.ErrMessageActionWindowExpired)
}
