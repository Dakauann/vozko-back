package callhistory

import (
	"testing"

	"vozko/domain/callrouting"
	"vozko/domain/calls/cdr"
)

func TestACallIsVisibleToItsParticipantsAndToWhoeverSeesTheTeam(t *testing.T) {
	agent := "u-agent"
	answered := start
	placed := cdr.Call{CallID: "c1", Direction: cdr.DirectionOutbound, AgentID: &agent}
	taken := cdr.Call{CallID: "c2", Direction: cdr.DirectionInbound, AgentID: &agent, AnsweredAt: &answered}
	transferred := []callrouting.TransferRecord{{CallID: "c1", FromUserID: agent, Target: callrouting.TransferTarget{UserID: "u-target"}}}
	cases := []struct {
		name         string
		userID       string
		seesEveryone bool
		call         cdr.Call
		transfers    []callrouting.TransferRecord
		want         bool
	}{
		{"the agent who placed it", agent, false, placed, nil, true},
		{"the agent who answered it", agent, false, taken, nil, true},
		{"the colleague it was passed to", "u-target", false, placed, transferred, true},
		{"a colleague outside the call", "u-other", false, placed, transferred, false},
		{"a manager who sees the team", "u-other", true, placed, nil, true},
		{"nobody signed in", "", false, placed, nil, false},
		{"an inbound call nobody took", agent, false, cdr.Call{CallID: "c3", Direction: cdr.DirectionInbound}, nil, false},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if got := Visible(tc.userID, tc.seesEveryone, tc.call, tc.transfers); got != tc.want {
				t.Fatalf("Visible = %v, want %v", got, tc.want)
			}
		})
	}
}
