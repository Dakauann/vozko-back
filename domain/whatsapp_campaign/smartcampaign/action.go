package smartcampaign

type Action string

const (
	Continue Action = "continue"
	Wait     Action = "wait"
	SlowDown Action = "slow_down"
	Pause    Action = "pause"
	Stop     Action = "stop"
	Complete Action = "complete"
)

var severity = map[Action]int{Continue: 0, Wait: 1, SlowDown: 2, Pause: 3, Stop: 4}

func (a Action) Holds() bool {
	return a == Pause || a == Stop
}

func (a Action) Releases() bool {
	return a == Continue || a == SlowDown
}

func (a Action) protects() bool {
	return a == SlowDown || a.Holds()
}

func Strongest(findings []Finding) Action {
	strongest := Continue
	for _, f := range findings {
		if severity[f.Action] > severity[strongest] {
			strongest = f.Action
		}
	}
	return strongest
}
