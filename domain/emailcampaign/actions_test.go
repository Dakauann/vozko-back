package emailcampaign

import (
	"slices"
	"testing"
)

func TestAllowedActionsPerStatus(t *testing.T) {
	cases := map[Status][]Command{
		StatusDraft:     {CommandEdit, CommandDelete, CommandReady, CommandSchedule, CommandLaunch},
		StatusReady:     {CommandEdit, CommandDelete, CommandSchedule, CommandLaunch, CommandStart},
		StatusScheduled: {CommandEdit, CommandSchedule, CommandUnschedule, CommandLaunch, CommandStop},
		StatusRunning:   {CommandPause, CommandStop},
		StatusPaused:    {CommandResume, CommandStop},
		StatusHeld:      {CommandResume, CommandPause, CommandStop},
		StatusCompleted: {},
		StatusStopped:   {},
	}
	for status, want := range cases {
		if got := AllowedActions(status); !slices.Equal(got, want) {
			t.Errorf("AllowedActions(%q) = %v, want %v", status, got, want)
		}
	}
	if got := AllowedActions("bogus"); len(got) != 0 {
		t.Fatalf("an unknown status allows nothing, got %v", got)
	}
}

func TestAllowedActionsFollowTheTransitionTable(t *testing.T) {
	transitionOf := map[Command]Action{
		CommandReady: ActionReady, CommandSchedule: ActionSchedule, CommandUnschedule: ActionUnschedule,
		CommandLaunch: ActionStart, CommandStart: ActionStart, CommandResume: ActionStart, CommandPause: ActionPause, CommandStop: ActionStop,
	}
	for _, status := range AllStatuses() {
		allowed := AllowedActions(status)
		for command, action := range transitionOf {
			_, err := Transition(status, action)
			want := err == nil
			switch command {
			case CommandLaunch:
				want = want && status.Editable()
			case CommandStart:
				want = want && status == StatusReady
			case CommandResume:
				want = want && status.Launched()
			}
			if got := slices.Contains(allowed, command); got != want {
				t.Errorf("%q allows %q = %v, but the transition table says %v", status, command, got, want)
			}
		}
		if slices.Contains(allowed, CommandEdit) != status.Editable() {
			t.Errorf("%q edit must follow Editable", status)
		}
		if slices.Contains(allowed, CommandDelete) != status.Deletable() {
			t.Errorf("%q delete must follow Deletable", status)
		}
	}
}

func TestEveryCommandIsKnown(t *testing.T) {
	for _, c := range AllCommands() {
		if !c.Valid() {
			t.Errorf("%q must be valid", c)
		}
	}
	if Command("jump").Valid() || Command("validate").Valid() {
		t.Fatal("unknown commands are invalid")
	}
}
