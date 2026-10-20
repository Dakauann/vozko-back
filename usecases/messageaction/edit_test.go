package messageaction

import (
	"context"
	"errors"
	"testing"
	"time"

	"vozko/domain/conversation"
	"vozko/domain/shared"
)

var bruno = shared.Person{UserID: "user-bruno"}

func TestEditingChangesTheProviderThenTheStoredText(t *testing.T) {
	f := newActionFixture(t, ownTelegramText("m1", actionNow.Add(-5*time.Minute)))

	got, err := f.uc.Edit.Execute(context.Background(), ana, target("m1"), " corrigido ")
	if err != nil {
		t.Fatal(err)
	}
	if len(f.channel.calls) != 1 || f.channel.calls[0] != "edit:pm-m1:corrigido" {
		t.Fatalf("provider calls = %v", f.channel.calls)
	}
	if got.Text != "corrigido" || got.EditedAt == nil || !got.EditedAt.Equal(actionNow) {
		t.Fatalf("stored text %q edited %v", got.Text, got.EditedAt)
	}
	if len(f.announcer.updated) != 1 {
		t.Fatalf("announced %v, want the open thread told", f.announcer.updated)
	}
}

func TestAnEditPastTheChannelWindowIsRefused(t *testing.T) {
	f := newActionFixture(t, ownTelegramText("m1", actionNow.Add(-time.Hour)))

	_, err := f.uc.Edit.Execute(context.Background(), ana, target("m1"), "corrigido")
	requireError(t, err, conversation.ErrMessageActionWindowExpired)
	if len(f.channel.calls) != 0 {
		t.Fatal("a stale client edited past the window")
	}
	f.requireNothingChanged(t)
}

func TestOnlyTheAuthorEditsTheirMessage(t *testing.T) {
	f := newActionFixture(t, ownTelegramText("m1", actionNow.Add(-time.Minute)))

	_, err := f.uc.Edit.Execute(context.Background(), bruno, target("m1"), "corrigido")
	requireError(t, err, conversation.ErrMessageActionNotAllowed)
	f.requireNothingChanged(t)
}

func TestAnEditNeedsText(t *testing.T) {
	f := newActionFixture(t, ownTelegramText("m1", actionNow.Add(-time.Minute)))

	_, err := f.uc.Edit.Execute(context.Background(), ana, target("m1"), "   ")
	requireError(t, err, conversation.ErrMessageEditTextInvalid)
	if len(f.channel.calls) != 0 {
		t.Fatal("an empty edit reached the provider")
	}
}

func TestAnEditTheProviderRefusesChangesNothing(t *testing.T) {
	f := newActionFixture(t, ownTelegramText("m1", actionNow.Add(-time.Minute)))
	f.channel.fail = errors.New("telegram said no")

	if _, err := f.uc.Edit.Execute(context.Background(), ana, target("m1"), "corrigido"); err == nil {
		t.Fatal("the provider failure must surface")
	}
	f.requireNothingChanged(t)
}
