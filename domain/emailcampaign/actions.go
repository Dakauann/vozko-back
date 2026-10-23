package emailcampaign

type Command string

const (
	CommandEdit       Command = "edit"
	CommandDelete     Command = "delete"
	CommandReady      Command = "ready"
	CommandSchedule   Command = "schedule"
	CommandUnschedule Command = "unschedule"
	CommandLaunch     Command = "launch"
	CommandStart      Command = "start"
	CommandResume     Command = "resume"
	CommandPause      Command = "pause"
	CommandStop       Command = "stop"
)

func AllCommands() []Command {
	return []Command{CommandEdit, CommandDelete, CommandReady, CommandSchedule, CommandUnschedule, CommandLaunch, CommandStart, CommandResume, CommandPause, CommandStop}
}

func (c Command) Valid() bool {
	for _, known := range AllCommands() {
		if c == known {
			return true
		}
	}
	return false
}

var commandTransitions = map[Command]Action{
	CommandReady:      ActionReady,
	CommandSchedule:   ActionSchedule,
	CommandUnschedule: ActionUnschedule,
	CommandLaunch:     ActionStart,
	CommandStart:      ActionStart,
	CommandResume:     ActionStart,
	CommandPause:      ActionPause,
	CommandStop:       ActionStop,
}

func AllowedActions(s Status) []Command {
	out := []Command{}
	if !s.Valid() {
		return out
	}
	for _, c := range AllCommands() {
		if allows(s, c) {
			out = append(out, c)
		}
	}
	return out
}

func allows(s Status, c Command) bool {
	switch c {
	case CommandEdit:
		return s.Editable()
	case CommandDelete:
		return s.Deletable()
	case CommandLaunch:
		return s.Editable() && transitions(s, ActionStart)
	case CommandStart:
		return s == StatusReady && transitions(s, ActionStart)
	case CommandResume:
		return s.Launched() && transitions(s, ActionStart)
	}
	action, ok := commandTransitions[c]
	return ok && transitions(s, action)
}

func transitions(s Status, a Action) bool {
	_, err := Transition(s, a)
	return err == nil
}
