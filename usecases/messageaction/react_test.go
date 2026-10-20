package messageaction

import (
	"context"
	"errors"
	"testing"

	"vozko/domain/conversation"
	"vozko/domain/shared"
)

func TestReactingSendsTheReactionThenStoresItAsTheOperators(t *testing.T) {
	f := newActionFixture(t, customerTelegramText("m1"))

	got, err := f.uc.React.Execute(context.Background(), ana, target("m1"), " 👍 ")
	if err != nil {
		t.Fatal(err)
	}
	if len(f.channel.calls) != 1 || f.channel.calls[0] != "react:pm-m1:👍" {
		t.Fatalf("provider calls = %v", f.channel.calls)
	}
	reactions := got.Reactions()
	if len(reactions) != 1 || reactions[0] != (conversation.Reaction{Emoji: "👍", Direction: conversation.ReactionOutbound, ActorID: "user-ana", At: actionNow}) {
		t.Fatalf("reactions = %+v", reactions)
	}
	if len(f.store.saved) != 1 || len(f.announcer.updated) != 1 || f.announcer.updated[0] != "m1" {
		t.Fatalf("saved %d, announced %v; want the change stored and sent to the open thread", len(f.store.saved), f.announcer.updated)
	}
	if f.access.asked[0] != "conversations:send" {
		t.Fatalf("asked %v, want the same permission sending needs", f.access.asked)
	}
}

func TestAnEmptyEmojiRemovesTheOperatorsReaction(t *testing.T) {
	reacted := customerTelegramText("m1")
	if err := reacted.React(conversation.Reaction{Emoji: "👍", Direction: conversation.ReactionOutbound, ActorID: "user-ana", At: actionNow}); err != nil {
		t.Fatal(err)
	}
	if err := reacted.React(conversation.Reaction{Emoji: "😀", Direction: conversation.ReactionInbound, ActorID: "5511", At: actionNow}); err != nil {
		t.Fatal(err)
	}
	f := newActionFixture(t, reacted)

	got, err := f.uc.React.Execute(context.Background(), ana, target("m1"), "")
	if err != nil {
		t.Fatal(err)
	}
	if len(f.channel.calls) != 1 || f.channel.calls[0] != "unreact:pm-m1" {
		t.Fatalf("provider calls = %v", f.channel.calls)
	}
	if reactions := got.Reactions(); len(reactions) != 1 || reactions[0].Direction != conversation.ReactionInbound {
		t.Fatalf("reactions = %+v, want only the customer reaction left", reactions)
	}
}

func TestAReactionTheProviderRefusesChangesNothing(t *testing.T) {
	f := newActionFixture(t, customerTelegramText("m1"))
	f.channel.fail = errors.New("telegram said no")

	if _, err := f.uc.React.Execute(context.Background(), ana, target("m1"), "👍"); err == nil {
		t.Fatal("the provider failure must surface")
	}
	f.requireNothingChanged(t)
}

func TestReactingFollowsTheSendAuthorization(t *testing.T) {
	cases := map[string]func(*actionFixture){
		"without conversations:send": func(f *actionFixture) { f.access.permission = false },
		"outside the operator scope": func(f *actionFixture) { f.access.entry = false },
	}
	for name, change := range cases {
		t.Run(name, func(t *testing.T) {
			f := newActionFixture(t, customerTelegramText("m1"))
			change(f)

			_, err := f.uc.React.Execute(context.Background(), ana, target("m1"), "👍")
			requireError(t, err, conversation.ErrUnauthorized)
			if len(f.channel.calls) != 0 {
				t.Fatal("an unauthorized reaction reached the provider")
			}
			f.requireNothingChanged(t)
		})
	}
}

func TestAMessageOfAnotherConversationIsNotFound(t *testing.T) {
	other := customerTelegramText("m1")
	other.EntryID = "entry-2"
	f := newActionFixture(t, other)

	_, err := f.uc.React.Execute(context.Background(), ana, target("m1"), "👍")
	requireError(t, err, conversation.ErrMessageNotFound)

	_, err = f.uc.React.Execute(context.Background(), ana, target("missing"), "👍")
	requireError(t, err, conversation.ErrMessageNotFound)
}

func TestAChannelWithoutAnActionRefusesIt(t *testing.T) {
	webchat := customerTelegramText("m1")
	webchat.EntryType = shared.EntryTypeWebchat
	f := newActionFixture(t, webchat)
	in := target("m1")
	in.EntryType = shared.EntryTypeWebchat

	_, err := f.uc.React.Execute(context.Background(), ana, in, "👍")
	requireError(t, err, conversation.ErrMessageActionUnsupported)
}

func TestAnInvalidTargetIsRefusedBeforeAnyLookup(t *testing.T) {
	cases := map[string]struct {
		change func(*conversation.MessageTarget)
		want   error
	}{
		"no message":      {func(in *conversation.MessageTarget) { in.MessageID = " " }, conversation.ErrMessageIDRequired},
		"no conversation": {func(in *conversation.MessageTarget) { in.EntryID = "" }, conversation.ErrEntryIDRequired},
		"unknown channel": {func(in *conversation.MessageTarget) { in.EntryType = "pigeon" }, conversation.ErrEntryTypeInvalid},
	}
	for name, tc := range cases {
		t.Run(name, func(t *testing.T) {
			f := newActionFixture(t, customerTelegramText("m1"))
			in := target("m1")
			tc.change(&in)

			_, err := f.uc.React.Execute(context.Background(), ana, in, "👍")
			requireError(t, err, tc.want)
		})
	}
}
